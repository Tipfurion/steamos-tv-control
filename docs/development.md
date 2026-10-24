# Development

The application uses Go 1.25, Bubble Tea v2 and Google TV Remote v2. One binary
provides automatic terminal onboarding (`configure`), advanced TUI settings
(`setup`), CLI controls and a Linux user daemon. Dependencies are pinned.

## Build and validate

```bash
make test
make lint
make build
make release VERSION=v0.1.0
make test-docker
make test-systemd
```

Release builds disable CGO and target Linux amd64. The archive contains only the
executable and is accompanied by SHA256SUMS. Apple extended attributes are
excluded when packaging on macOS. The release workflow publishes tagged builds,
including a version-pinned installer. See [releasing.md](releasing.md).
A public installation URL requires pushing the source and creating a first release;
neither is performed by local builds.

## Installation and state

The installer does not remount SteamOS or install distribution packages. It
installs under ~/.local/bin and ~/.config/systemd/user. Configuration, identity
and boot/toggle state use XDG config/data/state directories. Private files have
owner-only permissions. Reinstallation preserves the configuration and identity.

On first installation, configure discovers devices, asks for the screen code,
identifies the TV, selects the TCL power profile automatically, and learns a
network MAC from the Linux neighbor table when available. HDMI is enabled only
after visual confirmation. Enter skips HDMI without opening the TUI.

Linger enables boot startup and persistence across login sessions. Authorization
may be requested for this shared user setting. Uninstallation does not disable it.

## Power policy

The daemon watches logind PrepareForSleep and shutdown metadata. A delay
inhibitor bounds pre-sleep/stop work to at most two seconds within logind's
budget. Without shutdown metadata, the daemon does not guess poweroff versus
reboot. Network reconnects and service restarts do not trigger another wake.

Discrete power uses WAKEUP/SLEEP. TCL firmware tested here ignores SLEEP;
guarded-power uses POWER only after a fresh state report and persists unresolved
toggles to prevent blind repeats across process restarts. HDMI sequences are
restricted to input/navigation keys. Changing one invalidates its verification.

## Logs

Use journalctl --user -u steamos-tv-control -f. Logs include PC events, TV state,
action, confirmation, elapsed milliseconds, retries and errors. Under journald,
syslog prefixes supply priority and duplicate timestamps are omitted. Low-level
pairing protobuf logs are suppressed so pairing secrets never enter the journal.

For debug logging, add the following with systemctl --user edit steamos-tv-control:

```ini
[Service]
Environment=STEAMOS_TV_CONTROL_LOG_LEVEL=debug
```

Restart the service after changing the override. Default INFO logging records
state transitions; repeated retry details and connection timings use DEBUG.

## macOS hardware testing

```bash
bash scripts/build-macos.sh
open -W 'dist/SteamOS TV Control.app' --args discover
'dist/SteamOS TV Control.app/Contents/MacOS/steamos-tv-control' pair --host TV_IP
```

Allow Local Network access for the app. LaunchServices registration is needed
after rebuilding the app on the macOS version tested here; otherwise a local
connection can fail with no route to host. The daemon requires Linux.

Use isolated XDG directories for hardware credentials and keep them out of git.
See [testing.md](testing.md) and [test-results.md](test-results.md).
