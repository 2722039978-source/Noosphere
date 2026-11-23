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
  # A portal activated on the private D-Bus can still be unmounting its
  # document FUSE filesystem after dbus-run-session returns.
  for _ in {1..50}; do
    if ! mountpoint -q "$scratch/runtime/doc" \
      && rm -rf --one-file-system -- "$scratch" 2>/dev/null \
      && [[ ! -e "$scratch" ]]; then
      return 0
    fi
    sleep 0.1
  done
  printf 'The temporary Wayland session did not release %s\n' "$scratch" >&2
  return 1
}
trap 'status=$?; cleanup || exit 1; exit "$status"' EXIT
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
