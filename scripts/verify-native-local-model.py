import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import tempfile
import time
from urllib.error import URLError
from urllib.request import ProxyHandler, Request, build_opener


backend_path, server_path, weights_path, version = sys.argv[1:]
http = build_opener(ProxyHandler({}))


def request(url, body=None, token=None, timeout=10):
    headers = {"Content-Type": "application/json"}
    if token is not None:
        headers["Authorization"] = "Bearer " + token
    data = json.dumps(body).encode() if body is not None else None
    with http.open(Request(url, data=data, headers=headers), timeout=timeout) as response:
        return response.status, json.load(response)


def stop(child):
    if child.poll() is None:
        child.terminate()
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait(timeout=10)


with tempfile.TemporaryDirectory(prefix="noosphere-真实本地模型-") as scratch:
    root = Path(scratch)
    config = root / "config.yaml"
    config.write_text(json.dumps({
        "modules": {key: {"enabled": False} for key in ("codelens", "nebula", "devops")},
        "model_lab": {"enabled": False},
    }), encoding="utf-8")
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        local_port = probe.getsockname()[1]
    model_alias = "noosphere-cpu-" + secrets.token_hex(4)
    loopback = f"http://127.0.0.1:{local_port}"
    engine_logs = root / "llama.log"
    with engine_logs.open("wb") as log:
        server = subprocess.Popen([
            server_path, "--model", weights_path, "--host", "127.0.0.1", "--port", str(local_port),
            "--alias", model_alias, "--threads", "2", "--ctx-size", "512", "--n-gpu-layers", "0",
            "--parallel", "1",
        ], stdout=log, stderr=log)
        backend = None
        try:
            deadline = time.monotonic() + 60
            while True:
                try:
                    status, catalog = request(loopback + "/v1/models", timeout=2)
                    if status == 200 and {item["id"] for item in catalog["data"]} == {model_alias}:
                        break
                except (URLError, TimeoutError, ConnectionError):
                    pass
                if server.poll() is not None or time.monotonic() >= deadline:
                    raise RuntimeError("Local llama.cpp did not load the pinned test model")
                time.sleep(0.25)

            token = secrets.token_hex(32)
            env = dict(os.environ, NOOSPHERE_DESKTOP_MANAGED="1", NOOSPHERE_BACKEND_TOKEN=token)
            backend = subprocess.Popen([backend_path, "-config", config], cwd=root, env=env,
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
            backend.stdin.write("start\n")
            backend.stdin.flush()
            for line in backend.stdout:
                if line.startswith("NOOSPHERE_READY "):
                    base = "http://127.0.0.1:" + str(int(line.split()[1]))
                    break
            else:
                raise RuntimeError("Packaged backend exited before reporting readiness")
            status, health = request(base + "/health", token=token)
            assert status == 200 and health["version"] == version
            status, _ = request(base + "/api/gateway/models", {
                "id": "native-local-test", "name": "Packaged local model test",
                "provider": "openai_compatible", "base_url": loopback + "/v1",
                "model_name": model_alias, "strict_credential_origin": True,
            }, token=token)
            assert status == 201
            status, answer = request(base + "/api/gateway/chat", {
                "model_id": "native-local-test",
                "messages": [{"role": "user", "content": "Write a short greeting."}],
                "max_tokens": 24,
            }, token=token, timeout=45)
            assert status == 200 and answer["model"] == model_alias
            assert answer["content"].strip() and answer["tokens_used"] > 0
            assert server.poll() is None
            print(json.dumps({"app_version": version, "engine": "llama.cpp", "execution": "CPU",
                              "protocol": answer["provider"], "tokens_used": answer["tokens_used"],
                              "reply": answer["content"].strip()}, ensure_ascii=False))
        except Exception:
            print(engine_logs.read_text(encoding="utf-8", errors="replace")[-6000:], file=sys.stderr)
            raise
        finally:
            if backend is not None:
                backend.stdin.close()
                try:
                    backend.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    stop(backend)
            stop(server)
