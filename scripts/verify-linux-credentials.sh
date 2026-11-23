#!/usr/bin/env bash
set -euo pipefail

backend="$(realpath "${1:?Expected the installed Linux backend executable}")"
test -x "$backend"
scratch="$(mktemp -d -t noosphere-secret-service-XXXXXX)"
trap 'rm -rf -- "$scratch"' EXIT
export XDG_DATA_HOME="$scratch/data" XDG_CONFIG_HOME="$scratch/config" XDG_CACHE_HOME="$scratch/cache"
export XDG_RUNTIME_DIR="$scratch/runtime"
install -d -m 700 "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$XDG_RUNTIME_DIR" "$scratch/backend"
unset GNOME_KEYRING_CONTROL

# Start a private bus before either process. The keyring and backend only see
# the disposable test data, and the model endpoint is a loopback placeholder.
dbus-run-session -- bash -s "$backend" "$scratch/backend" <<'BASH'
set -euo pipefail
gnome-keyring-daemon --foreground --unlock --components=secrets \
  --control-directory="$XDG_RUNTIME_DIR/keyring" <<< 'noosphere-fixture-passphrase' &
keyring=$!
trap 'kill "$keyring" 2>/dev/null || true; wait "$keyring" 2>/dev/null || true' EXIT

bus() { busctl --address="$DBUS_SESSION_BUS_ADDRESS" "$@"; }
service=org.freedesktop.secrets
object=/org/freedesktop/secrets
identity() { bus call org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus GetConnectionUnixProcessID s "$service"; }
for _ in {1..50}; do
  if [[ "$(identity 2>/dev/null)" == "u $keyring" ]]; then break; fi
  sleep 0.1
done
test "$(identity)" = "u $keyring"
alias_path() { bus call "$service" "$object" org.freedesktop.Secret.Service ReadAlias s "$1" | cut -d '"' -f 2; }
collection="$(alias_path default)"
if [[ "$collection" == / ]]; then
  collection="$(alias_path login)"
  [[ "$collection" == "$object"/collection/* ]]
  bus call "$service" "$object" org.freedesktop.Secret.Service SetAlias so default "$collection"
fi
[[ "$collection" == "$object"/collection/* ]]
test "$(bus get-property "$service" "$collection" org.freedesktop.Secret.Collection Locked)" = 'b false'

timeout 60 python3 - "$1" "$2" <<'PY'
from contextlib import contextmanager
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
from urllib.request import build_opener, ProxyHandler, Request

executable = sys.argv[1]
state = Path(sys.argv[2])
config = state / "config.yaml"
config.write_text(json.dumps({
    "modules": {key: {"enabled": False} for key in ("codelens", "nebula", "devops")},
    "model_lab": {"enabled": False},
}))
opener = build_opener(ProxyHandler({}))
dummy = "not-a-real-api-key-secret-service-fixture"

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
                raise RuntimeError("Installed backend exited before reporting readiness")

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
            if child.wait(timeout=10) != 0:
                raise RuntimeError("Installed backend failed to shut down normally")

with backend() as request:
    status, response = request({
        "id": "secret-fixture", "name": "Secret Service fixture", "provider": "openai_compatible",
        "base_url": "http://127.0.0.1:1/v1", "model_name": "fixture", "api_key": dummy,
        "strict_credential_origin": True,
    })
    assert status == 201 and response["added"]["id"] == "secret-fixture"
saved = (state / "models.yaml").read_text()
assert "encrypted_api_key: enc:v1:" in saved and dummy not in saved
with backend() as request:
    status, response = request()
    model, = response["models"]
    assert status == 200 and model["id"] == "secret-fixture" and "credential" not in model
assert (state / "models.yaml").read_text() == saved
print("Secret Service: installed backend saved an encrypted model credential and reopened it after restart")
PY
test "$(bus call "$service" "$object" org.freedesktop.Secret.Service SearchItems a{ss} \
  2 application noosphere credential Noosphere/GatewayKey | cut -d ' ' -f 1,2)" = 'aoao 1'
echo 'Secret Service: one unlocked Noosphere item exists on the private bus'
BASH
