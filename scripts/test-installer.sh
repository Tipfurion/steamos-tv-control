#!/usr/bin/env bash
set -euo pipefail
# Run inside the read-only Docker test image, as its ordinary test user.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export HOME="$work/home with spaces"
mkdir -p "$HOME" "$work/release" "$work/stubs"
export PATH="$work/stubs:$PATH"
export TEST_INSTALL_LOG="$work/actions"
# Installer selection is tested with an amd64 fixture even on Apple Silicon;
# the actual amd64 executable is smoke-tested separately by test-docker.sh.
cat > "$work/stubs/uname" <<'SH'
#!/bin/sh
if [ "$1" = -m ]; then echo x86_64; else /usr/bin/uname "$@"; fi
SH
cat > "$work/stubs/systemctl" <<'SH'
#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$TEST_INSTALL_LOG"
case "$*" in *is-active*) exit 1 ;; esac
exit 0
SH
cat > "$work/stubs/loginctl" <<'SH'
#!/bin/sh
printf 'loginctl %s\n' "$*" >> "$TEST_INSTALL_LOG"
SH
chmod +x "$work/stubs/"*
make_release() {
    version=$1
    cat > "$work/release/steamos-tv-control" <<SH
#!/bin/sh
case "\$1" in
  version) echo '$version' ;;
  configure)
    printf 'PAIRING_CODE: '
    IFS= read -r code
    [ "\$code" = fixture-code ] || exit 1
    mkdir -p "\$HOME/.config/steamos-tv-control"
    printf '{}\\n' > "\$HOME/.config/steamos-tv-control/config.json"
    ;;
  service) printf 'app service install\\n' >> "\$TEST_INSTALL_LOG" ;;
  status) test "\${TEST_UNHEALTHY:-0}" != 1 ;;
  *) exit 2 ;;
esac
SH
    chmod +x "$work/release/steamos-tv-control"
    tar -C "$work/release" -czf "$work/release/steamos-tv-control_linux_amd64.tar.gz" steamos-tv-control
    (cd "$work/release"; sha256sum steamos-tv-control_linux_amd64.tar.gz > SHA256SUMS)
}
export STEAMOS_TV_CONTROL_NO_SETUP=1
export STEAMOS_TV_CONTROL_VERSION=v0.0.1
export STEAMOS_TV_CONTROL_RELEASE_URL="file://$work/release"
make_release v0.0.1
# Exercise the same stdin pipeline as the public one-command installer.
curl -fsSL "file://$PWD/install.sh" | bash
binary="$HOME/.local/bin/steamos-tv-control"
[ "$("$binary" version)" = v0.0.1 ]
mkdir -p "$HOME/.config/steamos-tv-control"
printf 'preserve this configuration\n' > "$HOME/.config/steamos-tv-control/config.json"
export STEAMOS_TV_CONTROL_VERSION=v0.0.2
make_release v0.0.2
bash install.sh
[ "$("$binary" version)" = v0.0.2 ]
[ "$(cat "$HOME/.config/steamos-tv-control/config.json")" = 'preserve this configuration' ]
export STEAMOS_TV_CONTROL_VERSION=v0.0.3
make_release v0.0.3
export TEST_UNHEALTHY=1
if bash install.sh; then echo 'unhealthy update accepted' >&2; exit 1; fi
unset TEST_UNHEALTHY
[ "$("$binary" version)" = v0.0.2 ]
printf 'corrupt' >> "$work/release/steamos-tv-control_linux_amd64.tar.gz"
if bash install.sh; then echo 'corrupt archive accepted' >&2; exit 1; fi
[ "$("$binary" version)" = v0.0.2 ]
rm "$work/release/steamos-tv-control_linux_amd64.tar.gz"
if bash install.sh; then echo 'missing archive accepted' >&2; exit 1; fi
[ "$("$binary" version)" = v0.0.2 ]
printf '\nPASS: read-only root, spaces, install, upgrade, rollback, checksum, failed download\n'
make_release v0.0.4
export STEAMOS_TV_CONTROL_VERSION=v0.0.4
python3 scripts/test-install-prompt.py "$work/another home"
