#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Linux ]]; then
  echo 'Wayland verification requires a native Linux host.' >&2
  exit 1
fi
packages="$(realpath "${1:?Expected a versioned Linux package directory}")"
repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
scratch="$(mktemp -d -t noosphere-wayland-XXXXXX)"
compositor=''
cleanup() {
  if [[ -n "$compositor" ]]; then
    kill "$compositor" 2>/dev/null || true
    wait "$compositor" 2>/dev/null || true
  fi
  rm -rf -- "$scratch"
}
trap cleanup EXIT
export XDG_RUNTIME_DIR="$scratch/runtime" WAYLAND_DISPLAY=noosphere-ci-wayland
export XDG_CONFIG_HOME="$scratch/config" XDG_DATA_HOME="$scratch/data" XDG_CACHE_HOME="$scratch/cache"
install -d -m 700 "$XDG_RUNTIME_DIR" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME"
unset DISPLAY WAYLAND_SOCKET
weston --backend=headless-backend.so --use-pixman --no-config --socket="$WAYLAND_DISPLAY" --idle-time=0 \
  >"$scratch/weston.log" 2>&1 &
compositor=$!
for _ in {1..100}; do
  if [[ -S "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY" ]] || ! kill -0 "$compositor" 2>/dev/null; then break; fi
  sleep 0.1
done
if [[ ! -S "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY" ]]; then
  cat "$scratch/weston.log" >&2
  exit 1
fi
weston --version
GDK_BACKEND=wayland dbus-run-session -- node "$repo/scripts/verify-native-unix.mjs" "$packages" --linux-wayland
