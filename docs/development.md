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

The daemon opens a persistent Remote v2 control session immediately at startup
and answers its keepalive pings. Refresh and standby reuse the ready socket:
NetworkManager begins disabling Wi-Fi concurrently with PrepareForSleep, so a
new TLS handshake at that point can miss the remaining network window. EOF and
idle expiry invalidate cached power. Resume discards the old socket before
obtaining a fresh power report and applying the wake policy. Pairing continues
to use the pinned upstream library; the control session owns framing, socket
deadlines, liveness and serialized writes.

On NetworkManager systems, service installation also uses sudo to install a
root-owned pre-down hook at
`/etc/NetworkManager/dispatcher.d/pre-down.d/90-steamos-tv-control-<uid>`.
SteamOS keeps `/etc` in a writable overlay, so the system image stays read-only.
NetworkManager waits for this hook before disconnecting the interface. The hook
checks logind's sleep state and calls the daemon's bounded `/network/pre-down`
endpoint, which respects AutoOff. It drops privileges before opening the user
socket and never executes a home-directory binary as root. Ordinary network
disconnects do not request standby. Uninstallation removes the hook with sudo;
unchanged hooks do not request authorization again on updates.

Abandoned control sockets use an abortive TCP close. An unconfirmed POWER must
not remain queued for delivery when Wi-Fi reconnects after resume.

Discrete power uses WAKEUP/SLEEP. TCL firmware tested here ignores SLEEP;
guarded-power uses POWER only after a fresh state report and persists unresolved
toggles to prevent blind repeats across process restarts. HDMI sequences are
restricted to input/navigation keys. Changing one invalidates its verification.

HDMI selection supports `direct` key commands, a navigation `sequence`, or an
`app-link` with an `input_uri` pointing to an Android TV passthrough input.
The link is bound to the configured HDMI port and is sent as a Remote v2 app
link request. It does not require ADB or menu navigation. Onboarding tries a
modern TCL input link if the direct key does not select the PC picture; the
user must confirm the alternative before automatic input selection is enabled.
The advanced TUI's `l` action edits the link for firmware with different IDs.

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
