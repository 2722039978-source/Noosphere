"""Windows loopback GenAI launcher. Stop only a captured PID plus creation identity."""
import argparse
from contextlib import contextmanager
import ctypes
from ctypes import wintypes
import datetime
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import time
import urllib.request
import uuid

K32 = ctypes.WinDLL("kernel32", use_last_error=True)
K32.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
K32.OpenProcess.restype = wintypes.HANDLE
K32.CloseHandle.argtypes = [wintypes.HANDLE]
K32.GetProcessTimes.argtypes = [wintypes.HANDLE] + [ctypes.POINTER(wintypes.FILETIME)] * 4
K32.QueryFullProcessImageNameW.argtypes = [wintypes.HANDLE, wintypes.DWORD, wintypes.LPWSTR, ctypes.POINTER(wintypes.DWORD)]
K32.TerminateProcess.argtypes = [wintypes.HANDLE, wintypes.UINT]
K32.CreateMutexW.argtypes = [ctypes.c_void_p, wintypes.BOOL, wintypes.LPCWSTR]
K32.CreateMutexW.restype = wintypes.HANDLE
K32.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
K32.WaitForSingleObject.restype = wintypes.DWORD
K32.ReleaseMutex.argtypes = [wintypes.HANDLE]
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + f".{os.getpid()}.tmp")
    temporary.write_text(json.dumps(value, indent=2, ensure_ascii=False), encoding="utf-8")
    temporary.replace(path)


@contextmanager
def control_lock(_port):
    # A deployment has one state file, even if a later launch chooses another
    # port. Serialize every OpenVINO start/stop across processes and ports.
    handle = K32.CreateMutexW(None, False, "Global\\NoosphereOpenVINO-Control-v1")
    if not handle: raise ctypes.WinError(ctypes.get_last_error())
    acquired = False
    try:
        status = K32.WaitForSingleObject(handle, 190000)
        if status not in (0, 0x80): raise TimeoutError("Another OpenVINO start/stop is still in progress")
        acquired = True
        yield
    finally:
        if acquired: K32.ReleaseMutex(handle)
        K32.CloseHandle(handle)


def handle_identity(handle):
    name = ctypes.create_unicode_buffer(32768)
    length = wintypes.DWORD(len(name))
    times = [wintypes.FILETIME() for _ in range(4)]
    if not K32.QueryFullProcessImageNameW(handle, 0, name, ctypes.byref(length)): return None
    if not K32.GetProcessTimes(handle, *(ctypes.byref(t) for t in times)): return None
    return {"exe": str(Path(name.value).resolve()), "creation_filetime": times[0].dwHighDateTime << 32 | times[0].dwLowDateTime}


def identity(pid):
    handle = K32.OpenProcess(0x1000, False, pid)
    if not handle: return None
    try: return handle_identity(handle)
    finally: K32.CloseHandle(handle)


def matches(actual, state, exe):
    expected = state.get("identity", {})
    return bool(actual and actual.get("creation_filetime") == expected.get("creation_filetime")
                and Path(actual["exe"]) == exe and Path(expected.get("exe", "")) == exe)


def owned_state(path, exe):
    if not path.exists(): return None
    state = json.loads(path.read_text(encoding="utf-8"))
    return state if matches(identity(state["pid"]), state, exe) else None


class MemoryStatus(ctypes.Structure):
    _fields_ = [("length", wintypes.DWORD), ("load", wintypes.DWORD)] + [(name, ctypes.c_ulonglong) for name in ("total_phys", "avail_phys", "total_page", "avail_page", "total_virtual", "avail_virtual", "avail_extended")]


def free_ram_gib():
    status = MemoryStatus(); status.length = ctypes.sizeof(status)
    if not K32.GlobalMemoryStatusEx(ctypes.byref(status)): raise ctypes.WinError(ctypes.get_last_error())
    return status.avail_phys / 2**30


def port_open(port):
    with socket.socket() as probe:
        probe.settimeout(0.3)
        return probe.connect_ex(("127.0.0.1", port)) == 0


def request(port, path, payload=None, timeout=10):
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(f"http://127.0.0.1:{port}{path}", data=data, headers={"Content-Type": "application/json"})
    with OPENER.open(req, timeout=timeout) as response: return json.load(response)


def stop_captured(state, exe):
    if not state: return {"stopped": False, "reason": "No owned OpenVINO process is running"}
    handle = K32.OpenProcess(0x0001 | 0x1000, False, state["pid"])
    try:
        if not handle or not matches(handle_identity(handle), state, exe):
            raise RuntimeError("PID identity changed; no process was stopped")
        if not K32.TerminateProcess(handle, 0): raise ctypes.WinError(ctypes.get_last_error())
    finally:
        if handle: K32.CloseHandle(handle)
    for _ in range(50):
        if not matches(identity(state["pid"]), state, exe): return {"stopped": True, "pid": state["pid"]}
        time.sleep(0.1)
    raise RuntimeError("Owned OpenVINO process did not exit within five seconds")


def start(args, exe, state_path):
    current = owned_state(state_path, exe)
    if current:
        if current.get("port") != args.port: raise RuntimeError("This deployment already owns a different port; stop it before changing ports")
        if current.get("device") != args.device: raise RuntimeError("Stop the owned server before changing CPU/GPU device")
        if current.get("num_ctx", 4096) != args.num_ctx or current.get("cpu_threads", 2) != args.cpu_threads:
            raise RuntimeError("Stop the owned server before changing context or threads")
        health = request(args.port, "/health")
        if health.get("instance") != current.get("instance"): raise RuntimeError("Owned server readiness identity differs")
        return {"already_running": True, **current}
    if port_open(args.port): raise RuntimeError("Port is already occupied; no existing process was stopped")
    if args.device != "CPU" and any(port_open(port) for port in (18787, 18788)):
        raise RuntimeError("Another local model endpoint is running. Stop it before loading the OpenVINO GPU model")
    before = free_ram_gib()
    if before < 2.0: raise RuntimeError(f"Available RAM {before:.3f} GiB is below the 2 GiB startup guard")
    source = Path(__file__).resolve().parent
    instance = uuid.uuid4().hex
    stamp = datetime.datetime.now().strftime("%Y%m%d-%H%M%S") + "-" + instance[:8]
    logs = args.runtime_home / "logs"; logs.mkdir(parents=True, exist_ok=True)
    command = [str(exe), "-I", "-S", str(source / "bootstrap.py"), str(args.runtime_home),
               "--model-dir", str(args.runtime_home / "models" / "Qwen3-0.6B-int4-ov"),
               "--manifest", str(source / "model.lock.json"), "--device", args.device,
               "--port", str(args.port), "--cpu-threads", str(args.cpu_threads), "--num-ctx", str(args.num_ctx), "--max-output", str(min(1024, args.num_ctx - 1)), "--instance", instance]
    state = None
    with (logs / (stamp + ".stdout.log")).open("wb") as out, (logs / (stamp + ".stderr.log")).open("wb") as err:
        process = subprocess.Popen(command, cwd=args.runtime_home, stdout=out, stderr=err, creationflags=subprocess.CREATE_NO_WINDOW)
    try:
        captured = identity(process.pid)
        if not captured or Path(captured["exe"]) != exe: raise RuntimeError("Could not verify the spawned server identity")
        state = {"pid": process.pid, "identity": captured, "instance": instance, "device": args.device, "port": args.port,
                 "num_ctx": args.num_ctx, "cpu_threads": args.cpu_threads,
                 "started_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "free_ram_before_gib": before,
                 "stdout": str(logs / (stamp + ".stdout.log")), "stderr": str(logs / (stamp + ".stderr.log"))}
        save(state_path, state)
        for _ in range(180):
            if process.poll() is not None: raise RuntimeError(f"Owned OpenVINO server exited: {process.returncode}; inspect {state['stderr']}")
            if free_ram_gib() < 0.75: raise RuntimeError("Available RAM dropped below the 0.75 GiB loading guard")
            try:
                health = request(args.port, "/health", timeout=1)
                if health.get("status") == "ok" and health.get("instance") == instance:
                    state["free_ram_ready_gib"] = free_ram_gib()
                    save(state_path, state)
                    save(logs / (stamp + ".metadata.json"), request(args.port, "/noosphere/openvino/metadata"))
                    return {"ready": True, "base_url": f"http://127.0.0.1:{args.port}/v1", **state}
            except (OSError, ValueError): pass
            time.sleep(1)
        raise TimeoutError("OpenVINO readiness exceeded 180 seconds")
    except BaseException:
        # Never reread a state file that could belong to another invocation.
        if state and matches(identity(state["pid"]), state, exe): stop_captured(state, exe)
        elif state is None and process.poll() is None:
            # Popen's retained process handle identifies only this child.
            process.terminate(); process.wait(timeout=5)
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("start", "stop", "status", "smoke"))
    parser.add_argument("--runtime-home", type=Path, default=Path(__file__).resolve().parents[2] / "model" / "OpenVINO")
    parser.add_argument("--device", choices=("GPU", "CPU", "GPU.0"), default="GPU")
    parser.add_argument("--port", type=int, default=18789)
    parser.add_argument("--cpu-threads", type=int, default=2)
    parser.add_argument("--num-ctx", type=int, default=4096)
    args = parser.parse_args(); args.runtime_home = args.runtime_home.resolve()
    if not 1024 <= args.port <= 65535 or not 1 <= args.cpu_threads <= 16 or not 512 <= args.num_ctx <= 8192:
        parser.error("Invalid port, CPU thread count, or context capacity")
    exe = Path(sys._base_executable).resolve()
    state_path = args.runtime_home / "logs" / "server-state.json"
    with control_lock(args.port):
        if args.action == "start": result = start(args, exe, state_path)
        elif args.action == "stop":
            result = stop_captured(owned_state(state_path, exe), exe)
            save(args.runtime_home / "logs" / "last-stop.json", result)
        elif args.action == "status": result = {"owned_state": owned_state(state_path, exe), "free_ram_gib": free_ram_gib()}
        else:
            current = owned_state(state_path, exe)
            if not current or request(args.port, "/health").get("instance") != current.get("instance"): raise RuntimeError("No ready owned server")
            result = request(args.port, "/noosphere/openvino/evaluate", {"model": "qwen3-0.6b-openvino-int4", "messages": [{"role": "user", "content": "Reply with only the number: 2+2=?"}], "options": {"temperature": 0, "seed": 42, "max_output_tokens": 32, "think": False}}, timeout=120)
            save(args.runtime_home / "logs" / f"smoke-{time.time_ns()}.json", result)
    print(json.dumps(result, ensure_ascii=True, indent=2))


if __name__ == "__main__":
    main()
