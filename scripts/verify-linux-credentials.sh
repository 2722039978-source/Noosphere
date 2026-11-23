#!/usr/bin/env bash
set -euo pipefail

backend="$(realpath "${1:?Expected the installed Linux backend executable}")"
test -x "$backend"
repo="$(cd -- "$(dirname -- "$0")/.." && pwd)"
scratch="$(mktemp -d -t noosphere-secret-service-XXXXXX)"
trap 'rm -rf -- "$scratch"' EXIT
export XDG_DATA_HOME="$scratch/data" XDG_CONFIG_HOME="$scratch/config" XDG_CACHE_HOME="$scratch/cache"
export XDG_RUNTIME_DIR="$scratch/runtime"
install -d -m 700 "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$XDG_RUNTIME_DIR" "$scratch/backend"
unset GNOME_KEYRING_CONTROL

# Start a private bus before either process. The keyring and backend only see
# the disposable test data, and the model endpoint is a loopback placeholder.
dbus-run-session -- bash -s "$backend" "$scratch/backend" "$repo/scripts/verify-native-credentials.py" <<'BASH'
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

timeout 60 python3 "$3" "$1" "$2" 'Secret Service'
test "$(bus call "$service" "$object" org.freedesktop.Secret.Service SearchItems a{ss} \
  2 application noosphere credential Noosphere/GatewayKey | cut -d ' ' -f 1,2)" = 'aoao 1'
echo 'Secret Service: one unlocked Noosphere item exists on the private bus'
BASH
