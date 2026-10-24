#!/usr/bin/env bash
set -euo pipefail

# The installer needs only tools already shipped by SteamOS. It never remounts
# the root filesystem or installs distribution packages.
fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }
[ "$(id -u)" != 0 ] || fail 'Run the installer as your normal user, without sudo.'
[ "$(uname -s)" = Linux ] || fail 'Automatic installation requires Linux/SteamOS.'
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; *) fail 'This release supports Linux amd64 only.' ;; esac
for tool in curl tar sha256sum awk mktemp systemctl loginctl; do
    command -v "$tool" >/dev/null || fail "Required system tool not found: $tool"
done
systemctl --user show-environment >/dev/null || fail 'No user session is available. Run this from a Desktop Mode terminal.'

repo=https://github.com/Tipfurion/steamos-tv-control
# Replaced only when packaging a release. Published installers never resolve
# "latest": their script, archive and checksums always belong to one version.
release_version='@RELEASE_VERSION@'
version=${STEAMOS_TV_CONTROL_VERSION:-$release_version}
if [ "$release_version" != '@RELEASE_VERSION@' ] && [ "$version" != "$release_version" ]; then
    fail "This installer belongs to $release_version. Download the installer for $version instead."
fi
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][a-zA-Z0-9.-]+)?$ ]] || fail 'Download install.sh from a published GitHub Release.'
base=${STEAMOS_TV_CONTROL_RELEASE_URL:-$repo/releases/download/$version}
asset="steamos-tv-control_linux_${arch}.tar.gz"
work=$(mktemp -d)
destination="$HOME/.local/bin/steamos-tv-control"
backup="$destination.previous"
staged="$destination.new.$$"
committed=0
replaced=0
was_active=0
if systemctl --user is-active --quiet steamos-tv-control.service; then was_active=1; fi
cleanup() {
    code=$?
    if [ "$committed" = 0 ] && [ "$replaced" = 1 ]; then
        if [ -f "$backup" ]; then
            mv -f "$backup" "$destination"
            if [ "$was_active" = 1 ]; then systemctl --user restart steamos-tv-control.service || true; fi
            printf 'Previous version restored.\n' >&2
        fi
    fi
    rm -rf "$work"
    rm -f "$staged"
    exit "$code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf 'Downloading SteamOS TV Control %s…\n' "$version"
curl -fsSL --retry 2 "$base/$asset" -o "$work/$asset"
curl -fsSL --retry 2 "$base/SHA256SUMS" -o "$work/SHA256SUMS"
checksum=$(awk -v file="$asset" '$2 == file { print $1 }' "$work/SHA256SUMS")
[[ "$checksum" =~ ^[0-9a-fA-F]{64}$ ]] || fail 'Release checksum not found.'
(cd "$work"; printf '%s  %s\n' "$checksum" "$asset" | sha256sum -c -)
[ "$(tar -tzf "$work/$asset")" = steamos-tv-control ] || fail 'Unexpected release archive contents.'
tar -xzf "$work/$asset" -C "$work" --no-same-owner
chmod 755 "$work/steamos-tv-control"
[ "$("$work/steamos-tv-control" version)" = "$version" ] || fail 'The downloaded application cannot run or has the wrong version.'

mkdir -p "$HOME/.local/bin"
cp "$work/steamos-tv-control" "$staged"
chmod 755 "$staged"
if [ -f "$destination" ]; then cp -p "$destination" "$backup"; fi
mv -f "$staged" "$destination"
replaced=1

config_dir=${XDG_CONFIG_HOME:-$HOME/.config}/steamos-tv-control
if [ ! -f "$config_dir/config.json" ]; then
    if [ "${STEAMOS_TV_CONTROL_NO_SETUP:-0}" != 1 ]; then
        "$destination" configure </dev/tty >/dev/tty 2>/dev/tty
        [ -f "$config_dir/config.json" ] || fail 'Configuration is incomplete. Run the installation command again.'
    fi
fi
"$destination" service install
if [ "$was_active" = 1 ]; then systemctl --user restart steamos-tv-control.service; fi
ready=0
for _ in 1 2 3 4 5; do
    if "$destination" status --json >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
done
[ "$ready" = 1 ] || fail 'Service is not ready. Details: journalctl --user -u steamos-tv-control -n 50'
committed=1
rm -f "$backup"
printf '\nInstalled: %s\nAdvanced settings: "%s" setup\nDiagnostics: "%s" doctor\n' "$version" "$destination" "$destination"
