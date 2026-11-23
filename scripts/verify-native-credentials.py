from contextlib import contextmanager
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
from urllib.request import build_opener, ProxyHandler, Request


executable, directory, label, *options = sys.argv[1:]
assert options in ([], ["--expect-locked"])
state = Path(directory)
config = state / "config.yaml"
models = state / "models.yaml"
opener = build_opener(ProxyHandler({}))
dummy = "not-a-real-api-key-native-credential-fixture"
model_id = "secret-fixture"


@contextmanager
def backend():
    token = secrets.token_hex(32)
    env = dict(os.environ, NOOSPHERE_DESKTOP_MANAGED="1", NOOSPHERE_BACKEND_TOKEN=token)
    with subprocess.Popen([executable, "-config", config], cwd=state, env=env,
                          stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True) as child:
        try:
            child.stdin.write("start\n")
            child.stdin.flush()
            for line in child.stdout:
                if line.startswith("NOOSPHERE_READY "):
                    base = "http://127.0.0.1:" + str(int(line.split()[1]))
                    break
            else:
                raise RuntimeError("Packaged backend exited before reporting readiness")

            def request(body=None):
                headers = {"Authorization": "Bearer " + token}
                if body is not None:
                    headers["Content-Type"] = "application/json"
                data = json.dumps(body).encode() if body is not None else None
                with opener.open(Request(base + "/api/gateway/models", data=data, headers=headers), timeout=10) as response:
                    return response.status, json.load(response)

            yield request
        finally:
            child.stdin.close()
            try:
                status = child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait()
                raise
            if status != 0:
                raise RuntimeError("Packaged backend failed to shut down normally")


if options:
    saved = models.read_bytes()
    with backend() as request:
        status, response = request()
        model, = response["models"]
        assert status == 200 and model["id"] == model_id
        credential = model["credential"]
        assert credential["provider"] == "macos_keychain" and credential["status"] == "locked", credential
    assert models.read_bytes() == saved
    print(f"{label}: backend reported locked storage and retained the encrypted credential")
else:
    config.write_text(json.dumps({
        "modules": {key: {"enabled": False} for key in ("codelens", "nebula", "devops")},
        "model_lab": {"enabled": False},
    }), encoding="utf-8")
    with backend() as request:
        status, response = request({
            "id": model_id, "name": "Native credential fixture", "provider": "openai_compatible",
            "base_url": "http://127.0.0.1:1/v1", "model_name": "fixture", "api_key": dummy,
            "strict_credential_origin": True,
        })
        assert status == 201 and response["added"]["id"] == model_id
    saved = models.read_bytes()
    assert b"encrypted_api_key: enc:v1:" in saved and dummy.encode() not in saved
    with backend() as request:
        status, response = request()
        model, = response["models"]
        assert status == 200 and model["id"] == model_id and "credential" not in model
    assert models.read_bytes() == saved
    print(f"{label}: packaged backend saved an encrypted model credential and reopened it after restart")
