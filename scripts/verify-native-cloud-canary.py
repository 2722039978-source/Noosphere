"""Run a temporary remote inference fixture or verify it using a packaged gateway."""

from contextlib import ExitStack
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import stat
import subprocess
import sys
import tarfile
import tempfile
from threading import Lock, Thread
import time
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, Request, build_opener
import zipfile


VERSION = "1.0.0"
SOURCE = "564ad1f93549b0a05d4cdfa6f19e5bbddfd6d5a0"
ALIAS = "noosphere-smollm2-cloud-canary"
TLS_ORIGIN = re.compile(r"https://[a-z0-9-]+\.trycloudflare\.com")
HTTP = build_opener(ProxyHandler({}))


def request(url, key=None, payload=None, timeout=8):
    headers = {"Authorization": "Bearer " + key} if key else {}
    data = None
    if payload is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(payload).encode()
    with HTTP.open(Request(url, headers=headers, data=data), timeout=timeout) as response:
        return json.load(response)


def stop(child):
    if child.poll() is None:
        child.terminate()
        try:
            child.wait(timeout=8)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait(timeout=8)


def serve(scratch, executable, weights, tunnel):
    scratch.mkdir(exist_ok=True)
    key = os.environ.pop("CLOUD_CANARY_KEY")
    if not re.fullmatch(r"[a-f0-9]{64}", key):
        raise RuntimeError("Invalid test key configuration")

    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    upstream = f"http://127.0.0.1:{port}"
    completed = 0
    lock = Lock()

    class Proxy(BaseHTTPRequestHandler):
        def do_GET(self):
            self.forward("/v1/models")

        def do_POST(self):
            self.forward("/v1/chat/completions")

        def log_message(self, *_args):
            pass

        def reply(self, status, body):
            data = json.dumps(body, ensure_ascii=False).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", len(data))
            self.end_headers()
            self.wfile.write(data)

        def forward(self, permitted):
            nonlocal completed
            if self.path != permitted:
                self.reply(404, {"error": {"message": "Route not found"}})
                return
            authorization = self.headers.get("Authorization", "").encode()
            if not secrets.compare_digest(authorization, ("Bearer " + key).encode()):
                self.reply(401, {"error": {"message": "Invalid authentication credentials"}})
                return
            try:
                payload = None
                if self.command == "POST":
                    length = int(self.headers.get("Content-Length", "0"))
                    if not 0 < length <= 4096:
                        raise ValueError("Body outside fixture limits")
                    payload = json.loads(self.rfile.read(length))
                    if payload.get("model") != ALIAS or payload.get("stream", False) is not False:
                        raise ValueError("Model or protocol outside fixture limits")
                    messages = payload.get("messages")
                    if not isinstance(messages, list) or len(messages) != 1:
                        raise ValueError("Messages outside fixture limits")
                answer = request(upstream + permitted, payload=payload, timeout=35)
                self.reply(200, answer)
                if payload is not None and answer["choices"][0]["message"]["content"].strip():
                    with lock:
                        completed += 1
                        print(f"Cloud engine generated answer {completed}.", flush=True)
            except (ValueError, TypeError, KeyError, IndexError):
                self.reply(400, {"error": {"message": "Unsupported inference request"}})
            except (URLError, TimeoutError):
                self.reply(502, {"error": {"message": "Test engine unavailable"}})

    with ExitStack() as resources:
        engine_logs = resources.enter_context((scratch / "engine.log").open("wb"))
        engine = subprocess.Popen([
            str(executable), "--model", str(weights), "--host", "127.0.0.1", "--port", str(port),
            "--alias", ALIAS, "--threads", "2", "--ctx-size", "512", "--n-gpu-layers", "0",
            "--parallel", "1",
        ], stdout=engine_logs, stderr=engine_logs)
        resources.callback(stop, engine)
        for _ in range(120):
            try:
                if request(upstream + "/v1/models", timeout=2)["data"][0]["id"] == ALIAS:
                    break
            except (URLError, TimeoutError):
                pass
            if engine.poll() is not None:
                raise RuntimeError("Pinned cloud inference engine exited")
            time.sleep(0.5)
        else:
            raise RuntimeError("Pinned cloud inference engine did not start")

        proxy = resources.enter_context(ThreadingHTTPServer(("127.0.0.1", 0), Proxy))
        worker = Thread(target=proxy.serve_forever, daemon=True)
        worker.start()
        resources.callback(proxy.shutdown)
        tunnel_log = scratch / "tunnel.log"
        tunnel_out = resources.enter_context(tunnel_log.open("wb"))
        tunnel_process = subprocess.Popen([
            str(tunnel), "tunnel", "--no-autoupdate", "--protocol", "http2",
            "--url", f"http://127.0.0.1:{proxy.server_port}",
        ], stdout=tunnel_out, stderr=tunnel_out)
        resources.callback(stop, tunnel_process)
        for _ in range(120):
            match = TLS_ORIGIN.search(tunnel_log.read_text(encoding="utf-8", errors="replace"))
            if match:
                origin = match.group()
                try:
                    if request(origin + "/v1/models", key, timeout=3)["data"][0]["id"] == ALIAS:
                        (scratch / "endpoint.txt").write_text(origin + "\n", encoding="utf-8")
                        break
                except (URLError, TimeoutError):
                    pass
            if tunnel_process.poll() is not None:
                raise RuntimeError("Temporary TLS tunnel exited")
            time.sleep(1)
        else:
            raise RuntimeError("Temporary TLS endpoint did not become ready")
        print("Temporary authenticated inference is available.", flush=True)
        deadline = time.monotonic() + 22 * 60
        while time.monotonic() < deadline:
            if engine.poll() is not None or tunnel_process.poll() is not None:
                raise RuntimeError("A canary process exited during validation")
            with lock:
                if completed >= 4:
                    break
            time.sleep(2)
        else:
            raise RuntimeError("The remote inference window expired before all four clients finished")
    (scratch / "done").touch()


def unpack_backend(package, target, root):
    backend = root / ("noosphere-backend.exe" if target == "windows-x64" else "noosphere-backend")
    if target == "linux-x64":
        process = subprocess.Popen(["dpkg-deb", "--fsys-tarfile", package], stdout=subprocess.PIPE)
        try:
            with tarfile.open(fileobj=process.stdout, mode="r|*") as archive:
                copy_tar_backend(archive, backend)
            with open(os.devnull, "wb") as discard:
                shutil.copyfileobj(process.stdout, discard)
        finally:
            process.stdout.close()
            if process.wait() != 0:
                raise RuntimeError("deb payload extraction failed")
    elif target == "windows-x64":
        with zipfile.ZipFile(package) as archive:
            matches = [entry for entry in archive.infolist() if not entry.is_dir()
                       and Path(entry.filename).name == backend.name]
            if len(matches) != 1:
                raise RuntimeError("The portable package must contain one backend")
            with archive.open(matches[0]) as source, backend.open("xb") as output:
                shutil.copyfileobj(source, output)
    else:
        with tarfile.open(package, mode="r:gz") as archive:
            copy_tar_backend(archive, backend)
    backend.chmod(backend.stat().st_mode | stat.S_IXUSR)
    return backend


def copy_tar_backend(archive, destination):
    found = False
    for entry in archive:
        if not entry.isfile() or Path(entry.name).name != destination.name:
            continue
        if found:
            raise RuntimeError("The package contains duplicate backends")
        with archive.extractfile(entry) as source, destination.open("xb") as output:
            shutil.copyfileobj(source, output)
        found = True
    if not found:
        raise RuntimeError("The packaged backend is missing")


def verify(package_root, origin, target, suffix, expected):
    if not TLS_ORIGIN.fullmatch(origin):
        raise RuntimeError("The endpoint does not match the temporary HTTPS service")
    key = os.environ.pop("CLOUD_CANARY_KEY")
    manifests = list(package_root.rglob("build-info.json"))
    if len(manifests) != 1:
        raise RuntimeError("Expected one pinned build manifest")
    metadata = json.loads(manifests[0].read_text(encoding="utf-8-sig"))
    if (metadata["version"], metadata["source_revision"], metadata["source_dirty"], metadata["target"]) != (
            VERSION, SOURCE, False, target):
        raise RuntimeError("Unexpected preview build identity")
    package = manifests[0].parent / f"Noosphere-{VERSION}-{target}.{suffix}"
    with package.open("rb") as artifact:
        digest = hashlib.file_digest(artifact, "sha256").hexdigest()
    if digest != expected:
        raise RuntimeError("The preview package hash changed")

    with tempfile.TemporaryDirectory(prefix="noosphere-远程模型-") as directory:
        root = Path(directory)
        backend = unpack_backend(package, target, root)
        config = root / "config.yaml"
        config.write_text(json.dumps({
            "modules": {name: {"enabled": False} for name in ("codelens", "nebula", "devops")},
            "model_lab": {"enabled": False},
        }), encoding="utf-8")
        private_token = secrets.token_hex(32)
        env = dict(os.environ, NOOSPHERE_DESKTOP_MANAGED="1", NOOSPHERE_BACKEND_TOKEN=private_token)
        with subprocess.Popen([backend, "-config", config], cwd=root, env=env,
                              stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True) as app:
            try:
                app.stdin.write("start\n")
                app.stdin.flush()
                for line in app.stdout:
                    if line.startswith("NOOSPHERE_READY "):
                        base = f"http://127.0.0.1:{int(line.split()[1])}"
                        break
                else:
                    raise RuntimeError("Packaged gateway did not report readiness")
                if request(base + "/health", private_token)["version"] != VERSION:
                    raise RuntimeError("The packaged gateway reported the wrong version")
                model = {"provider": "openai_compatible", "base_url": origin + "/v1", "model_name": ALIAS}
                path = base + "/api/gateway/models/probe"
                rejected = request(path, private_token, {"model": {**model, "api_key": "invalid-canary-key"}, "infer": True})
                if rejected.get("http_status") != 401 or rejected.get("inference") != "failed":
                    raise RuntimeError("The wrong credential did not produce an upstream authentication failure")
                reply = request(path, private_token, {"model": {**model, "api_key": key}, "infer": True}, 100)
                if reply.get("inference") != "succeeded" or reply.get("reported_model") != ALIAS:
                    raise RuntimeError("The packaged gateway did not confirm real remote inference")
                print(json.dumps({
                    "target": target, "source": SOURCE, "package_sha256": digest, "protocol": "HTTPS Chat Completions",
                    "remote_inference": "passed", "incorrect_credential_http": 401,
                    "engine": "llama.cpp b12359 CPU", "model": "SmolLM2-135M-Instruct Q4_K_M",
                }))
            finally:
                app.stdin.close()
                try:
                    app.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    stop(app)


if __name__ == "__main__":
    if sys.argv[1] == "serve":
        serve(*map(Path, sys.argv[2:]))
    elif sys.argv[1] == "verify":
        verify(Path(sys.argv[2]), *sys.argv[3:])
    else:
        raise SystemExit("Expected serve or verify")
