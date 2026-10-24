#!/usr/bin/env bash
set -euo pipefail
# Use only the dedicated OrbStack test machine. It is kept for inspection.
cd "$(dirname "$0")/.."
machine=steamos-tv-control-test
command -v orb >/dev/null || { echo 'OrbStack is required' >&2; exit 1; }
orb info "$machine" >/dev/null
make release VERSION=v0.0.0-test
orb -m "$machine" -u root sh -c 'command -v dbus-daemon; pacman -Q polkit >/dev/null 2>&1 || pacman -Sy --noconfirm polkit; systemctl start systemd-logind; loginctl enable-linger tvtest'
orb -m "$machine" -u tvtest bash -s <<'LINUX'
set -euo pipefail
systemctl --user stop steamos-tv-control.service || true
mkdir -p "$HOME/.local/bin" "$HOME/.config/steamos-tv-control" "$HOME/.local/share/steamos-tv-control/identity"
cp /work/dist/package/steamos-tv-control "$HOME/.local/bin/steamos-tv-control"
chmod 755 "$HOME/.local/bin/steamos-tv-control"
# No real TV commands are issued by this service test.
cat > "$HOME/.config/steamos-tv-control/config.json" <<'JSON'
{"version":1,"host":"127.0.0.1","hdmi":1,"input_mode":"direct","input_verified":false,"auto_on":false,"auto_off":false,"broadcast":"255.255.255.255:9","wake_seconds":1}
JSON
printf 'fixture: identity existence only\n' > "$HOME/.local/share/steamos-tv-control/identity/key.pem"
chmod 600 "$HOME/.local/share/steamos-tv-control/identity/key.pem"
"$HOME/.local/bin/steamos-tv-control" service install
systemctl --user is-active steamos-tv-control.service
for _ in 1 2 3 4 5; do
    if "$HOME/.local/bin/steamos-tv-control" status --json; then break; fi
    sleep 1
done
"$HOME/.local/bin/steamos-tv-control" status --json
systemctl --user restart steamos-tv-control.service
sleep 1
"$HOME/.local/bin/steamos-tv-control" status --json
"$HOME/.local/bin/steamos-tv-control" doctor --json
if "$HOME/.local/bin/steamos-tv-control" tv on >/dev/null 2>&1; then
    echo 'Offline fixture unexpectedly accepted a TV action' >&2
    exit 1
fi
journal=$(journalctl --user -u steamos-tv-control.service -p warning -g 'TV action failed' -o export --no-pager -n 1)
case "$journal" in *'PRIORITY=4'*) ;; *) echo 'Warning priority missing in journal' >&2; exit 1 ;; esac
journalctl --user -u steamos-tv-control.service -n 20 --no-pager
"$HOME/.local/bin/steamos-tv-control" service uninstall
if systemctl --user is-enabled steamos-tv-control.service; then exit 1; fi
echo 'PASS: real systemd install, linger, start, restart, IPC, logind, journal priorities, uninstall'
LINUX
