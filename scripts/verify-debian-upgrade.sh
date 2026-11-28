#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Linux || "$(dpkg --print-architecture)" != amd64 ]]; then
  echo 'Debian upgrade verification requires a native Linux amd64 runner.' >&2
  exit 1
fi

packages="$(realpath "${1:?Expected a versioned Linux package directory}")"
repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo"
version="$(cat scripts/build-config/VERSION)"
deb_version="$(node --input-type=module -e '
  import { debianPackageVersion } from "./scripts/desktop-release-manifest.mjs";
  console.log(debianPackageVersion(process.argv[1]));
' "$version")"
package=noosphere-desktop
desktop=/usr/bin/noosphere-desktop
candidate="$packages/Noosphere-$version-linux-x64.deb"
previous_version=1.0.1
previous_name="Noosphere-$previous_version-linux-x64.deb"
previous_sha256=77456e87e5ddcea46c27b3c013f991dfe70ee209d7997a1cb72bab287f1a38b9
dpkg --compare-versions "$previous_version" lt "$deb_version"
if dpkg-query -W "$package" &>/dev/null; then
  echo 'Debian upgrade verification requires a runner without an existing Noosphere package registration.' >&2
  exit 1
fi
(cd "$packages" && sha256sum --strict --check SHA256SUMS.txt)

scratch="$(mktemp -d -t noosphere-deb-upgrade-XXXXXX)"
trap 'rm -rf -- "$scratch"' EXIT
# apt's unprivileged download user must be able to read the public installer.
chmod 755 "$scratch"
previous="$scratch/$previous_name"
curl --fail --location --silent --show-error --connect-timeout 30 --max-time 300 --retry 3 --retry-all-errors --retry-max-time 300 \
  --output "$previous" "https://github.com/2722039978-source/Noosphere/releases/download/v$previous_version/$previous_name"
printf '%s  %s\n' "$previous_sha256" "$previous" | sha256sum --strict --check -

state="$scratch/用户数据 Workspace"
export XDG_DATA_HOME="$scratch/xdg-data" XDG_CONFIG_HOME="$scratch/xdg-config" XDG_CACHE_HOME="$scratch/xdg-cache"
export XDG_RUNTIME_DIR="$scratch/xdg-runtime"
install -d -m 700 "$state" "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$XDG_RUNTIME_DIR"
printf '%s\n' 'modules:' '  nebula:' '    embedding: {provider: mock}' > "$state/config.yaml"

install_package() {
  local file="$1" expected="$2" checksum
  test "$(dpkg-deb -f "$file" Package)" = "$package"
  test "$(dpkg-deb -f "$file" Architecture)" = amd64
  test "$(dpkg-deb -f "$file" Version)" = "$expected"
  sudo apt-get install -y --no-install-recommends "$file"
  test "$(dpkg-query -W -f='${Status}|${Version}|${Architecture}' "$package")" = "install ok installed|$expected|amd64"
  test "$(dpkg-query -S "$desktop")" = "$package: $desktop"
  test -x "$desktop"
  checksum="$(dpkg-deb --fsys-tarfile "$file" | tar -xOf - --wildcards --no-anchored "${desktop#/}" | sha256sum)"
  printf '%s  %s\n' "${checksum%% *}" "$desktop" | sha256sum --strict --check -
  printf '%s\n' "Installed $package $expected for amd64 at $desktop"
}

boot_installed() {
  xvfb-run -a dbus-run-session -- node scripts/verify-native-unix.mjs "$packages" --linux-installed "$state" "$1"
}

check_workspace_fixture() {
  python3 - "$state" "$1" "$2" <<'PY'
import json
from pathlib import Path
import sqlite3
import sys

root = Path(sys.argv[1])
mode, version = sys.argv[2:]
data_version = json.loads((root / "data-version.json").read_text())
assert data_version["app_version"] == version, data_version
database = root / "data/workspace.sqlite"
assert database.is_file(), database
record_id = "ci-debian-upgrade"
fixture_name = "Upgrade fixture 项目"
with sqlite3.connect(database) as connection:
    if mode == "seed":
        existing, = connection.execute("SELECT body FROM workspaces WHERE id = 'local'").fetchone()
        body = json.loads(existing)
        body.update(id=record_id, name=fixture_name)
        connection.execute("INSERT INTO workspaces(id, root_key, body) VALUES (?, '', ?)",
                           (record_id, json.dumps(body).encode()))
    body, = connection.execute("SELECT body FROM workspaces WHERE id = ?", (record_id,)).fetchone()
    assert json.loads(body)["name"] == fixture_name
PY
}

install_package "$previous" "$previous_version"
boot_installed "$previous_version"
check_workspace_fixture seed "$previous_version"
install_package "$candidate" "$deb_version"
check_workspace_fixture retained "$previous_version"
boot_installed "$version"
check_workspace_fixture retained "$version"
printf '%s\n' "Debian in-place upgrade $previous_version -> $deb_version retained the workspace and booted the installed app under X11"
