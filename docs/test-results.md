# Test results — October 24, 2026

## Verified

- macOS 26.5, Apple Silicon, Go 1.25.6: race-enabled tests and go vet.
- Local mutual TLS/protobuf server: framing, fragmented messages, power commands
  and reports; cancellation closes a silent pairing server.
- Isolated real D-Bus: logind signals, metadata, Unix FD passing, inhibitor
  release and reacquisition.
- Docker/Linux: race-enabled tests, vet and ShellCheck.
- Read-only container root with an ordinary user: installation, home path with
  spaces, update preserving configuration, rollback, corrupt checksum, missing
  download and an amd64 executable smoke test.
- Real Bubble Tea in a PTY: setup screen, Escape, menu, quit, no partial config.
- Dedicated Arch Linux amd64 machine, systemd 262 and polkit: actual user unit,
  linger, start/restart, IPC, logind and unit removal. Automation used a local
  fixture and did not send TV commands.
- After restarting the machine, the enabled service started automatically and
  the saved boot marker matched the new kernel boot ID.
- The real release archive built on macOS was installed through install.sh in
  Arch Linux, including checksum, extraction, service readiness and removal.
- First installation through curl-to-bash in a PTY completed the terminal code
  prompt without opening a full-screen wizard.
- Journal warning entries were verified with PRIORITY=4 on actual systemd.
- Release packaging checks cover installer/archive hashes, embedded version,
  pinned asset URLs and rejection of conflicting version overrides. GitHub
  Actions workflow syntax passes actionlint.

## TCL 65C7L, Living Room

Tested against the actual TV from macOS:

1. mDNS discovery, control port 6466 and pairing port 6467.
2. Screen-code pairing and saved client identity.
3. This firmware ignores SLEEP; the user confirmed the screen did not turn off.
4. Guarded POWER produced a standby report and visible screen-off confirmation.
5. Before changing the energy mode, Remote became unreachable in standby.
   Wake-on-LAN successfully restored the TV, confirmed by the user.
6. After the user enabled higher energy use, off/on succeeded with an empty MAC
   in the test configuration, proving no Wake-on-LAN was used.
7. With Wake-on-LAN still disabled, on/off/off/on/on all returned confirmed
   states; repeated commands preserved the requested state.

Initially macOS blocked the new executable with no route to host. The system
log reported local network blocking with a missing bundle ID. A registered app
bundle, Local Network permission and launch through LaunchServices resolved it.
Rebuilt app bundles need to be launched through open again on this system.

Test identities remain in git-ignored .test-results. No pairing code or private
key is included in this report.

## SteamOS 3.8.28 power regression

Tested over SSH on the actual SteamOS PC and TCL, using the identity paired on
SteamOS. The PC was not connected to an HDMI input during this check.

- Reproduced v0.1.1's failure: a POWER key sent immediately after RemoteStart
  was written successfully but ignored by the TV. A key sent later over the
  same connection worked. The user confirmed a visible off/on cycle.
- Adding a 250 ms wait after TCL connection initialization allowed POWER to
  work after a new connection in either direction, without Wake-on-LAN.
- The patched CLI, with the daemon stopped, confirmed `tv off` in 1.83 seconds
  and `tv on` in 0.56 seconds. No WOL backup was invoked.
- With the daemon running, an off/on cycle with five seconds in standby
  confirmed off in 1.88 seconds and on in 0.55 seconds, also without WOL.
- The `off, off, on, on` sequence preserved the requested power states. During
  this rapid turnaround the wake command needed the configured WOL backup;
  the log recorded that fallback explicitly, without repeating POWER.
- A late standby report now resolves a persisted pending-off transition before
  the next wake request. TLS deadlines no longer stop wake retries as if the
  pairing certificate had been rejected.
- Manual standby commands have a ten-second response budget. PC lifecycle
  actions retain the separate two-second inhibitor limit.
- Regression coverage includes a TLS TV simulator that ignores early keys,
  delayed confirmation across a controller restart, WOL after ignored or
  previously unconfirmed POWER, and timeout/authentication classification.
  Race-enabled tests, vet, Linux amd64 cross-compilation and Docker checks pass.

The power checks used the `dev-power-fix` build. The old binary and
configuration were backed up in the user's application state directory.

## TCL 65C7L HDMI 2

The PC was then connected to HDMI 2. SteamOS reported a connected and enabled
HDMI output. The following checks used Google TV Remote v2 with the identity
paired on SteamOS:

- TV_INPUT_HDMI_2 had no visible effect, including over an established
  connection and with separate press/release events.
- The app-link request below switched directly to HDMI 2. The user confirmed
  that it worked. ADB and source-menu navigation were not used.

```text
content://android.media.tv/passthrough/com.tcl.tvinput%2F.TvPassThroughService%2FHW16
```

- After installing `dev-tv-fix`, the regular `tv input 2` command sent this
  link. The user reported that HDMI 2 was selected.
- Starting from Google TV Home, the daemon completed `tv off`, five seconds
  of standby, and `tv on`. It confirmed standby in 1.62 seconds, on in
  0.56 seconds, then sent the HDMI 2 link. WOL was not invoked. The user
  confirmed that the TV turned on and displayed the PC picture.
- The link and HDMI port are saved, automatic input selection is enabled,
  and the user service is running with `dev-tv-fix`.
- Input-link tests cover exact protobuf URI delivery, configuration round-trip,
  the configured port, verified/unverified wake behavior, TCL onboarding
  fallback with visual confirmation, and changing ports in advanced settings.
  Race-enabled tests and vet pass on macOS and Linux, including Linux tests
  limited to two CPUs. The Linux amd64 binary was cross-compiled and installed.

Source selection has no protocol acknowledgement. Manual input status reports
`command_sent` with `confirmed: false`; visual verification enables the saved
method for automation without treating transport delivery as a source report.

## SteamOS suspend ordering regression

Real suspend exposed a separate network-ordering problem. NetworkManager began
disconnecting Wi-Fi concurrently with logind's PrepareForSleep signal, with the
interface disconnected about 70 ms later. Reconnecting at this point timed out
before sending standby. A persistent control socket sent POWER immediately,
but the user still observed standby only after resume; the log also showed a
late standby report after the resume action. Socket reuse alone did not pass
hardware acceptance.

The next build, `dev-sleep-order-fix`, adds a NetworkManager pre-down hook that
holds the connection while the daemon completes bounded standby. Installation
is part of `service install`, with sudo authorization for the root-owned hook
in SteamOS's writable `/etc` overlay. Normal disconnects and disabled AutoOff
do not request standby. Abandoned sockets are reset so old POWER data cannot
be delivered after the network returns.

Automated checks cover hook rendering and shell quoting, honoring AutoOff,
releasing the network on a deadline, abortive connection closure, persistent
socket reuse, peer EOF, reconnect and reset-before-resume ordering. Race tests,
vet and ShellCheck pass on macOS/Linux. Installation and real suspend with the
NetworkManager hook were then verified on the target PC:

- `service install` installed the matching hook as root:root with mode 0755,
  following the user's sudo authorization. The user service remained active.
- On the real suspend at 23:29:49, POWER was sent immediately. The pre-down
  request arrived 32 ms later and kept NetworkManager waiting. The TV reported
  standby and the controller confirmed off in 1.46 seconds.
- Wi-Fi disconnected about 51 ms after confirmed standby, demonstrating the
  required ordering on the actual interface.
- After resume, the controller established a fresh connection, confirmed on
  and sent the HDMI 2 link in 2.58 seconds. No late standby followed. The user
  reported that the cycle appeared to work.
- A second real cycle at 23:31 also passed: standby was confirmed in
  1.26 seconds, and resume confirmed on and selected HDMI 2 in 2.51 seconds.

The installed build is `dev-sleep-order-fix`. Two real suspend/resume cycles
passed with the NetworkManager hook. Longer repeated runs and long-duration
standby remain separate acceptance checks.

## Pending on target hardware

- Input selection from other external sources and repeated sleep/wake cycles.
- Real SteamOS boot/shutdown/reboot, ten suspend/resume cycles and mode switching.
- Persistence across SteamOS updates and wake after many hours in standby.

Container and VM results do not replace these hardware checks. The automatic
onboarding and expanded logging also pass race-enabled tests, vet, Linux Docker
checks and the dedicated systemd service test. The actual TCL reports
manufacturer TCL, so its compatible power profile is selected automatically.
