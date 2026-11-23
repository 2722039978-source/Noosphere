#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Darwin || "${GITHUB_ACTIONS:-}" != true || -z "${RUNNER_TEMP:-}" ]]; then
  echo 'macOS fixture verification requires an isolated GitHub Actions macOS runner.' >&2
  exit 1
fi
archive="$(realpath "${1:?Expected a verified macOS .app archive}")"
repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
scratch="$(mktemp -d "$RUNNER_TEMP/noosphere-keychain-XXXXXX")"
keychain="$scratch/noosphere-fixture.keychain-db"
created=0
changed=0
original=''
cleanup() {
  local status=$?
  if [[ $changed -eq 1 ]]; then
    security default-keychain -d user -s "$original" || status=1
  fi
  if [[ $created -eq 1 ]]; then
    security delete-keychain "$keychain" || status=1
  fi
  rm -rf -- "$scratch" || status=1
  exit "$status"
}
trap cleanup EXIT

mkdir "$scratch/搬迁 应用" "$scratch/测试 数据"
tar -xzf "$archive" -C "$scratch/搬迁 应用"
set -- "$scratch/搬迁 应用"/*.app
[[ $# -eq 1 && -d "$1" ]]
codesign --verify --deep --strict "$1"
backend="$1/Contents/Resources/bin/noosphere-backend"
test -x "$backend"

original="$(security default-keychain -d user | sed -n 's/^[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p')"
[[ -n "$original" && -f "$original" ]]
password="$(openssl rand -hex 24)"
security create-keychain -p "$password" "$keychain"
created=1
security unlock-keychain -p "$password" "$keychain"
unset password
security default-keychain -d user -s "$keychain"
changed=1
security default-keychain -d user | grep -F -- "$keychain" >/dev/null

python3 "$repo/scripts/verify-native-credentials.py" "$backend" "$scratch/测试 数据" 'macOS Keychain'
security find-generic-password -s 'Noosphere/GatewayKey' -a gateway-master-key "$keychain" >/dev/null
security lock-keychain "$keychain"
python3 "$repo/scripts/verify-native-credentials.py" "$backend" "$scratch/测试 数据" 'macOS Keychain' --expect-locked
