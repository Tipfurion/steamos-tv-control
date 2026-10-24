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
  Actions workflow syntax passes actionlint; publication itself remains pending.

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

## Pending on target hardware

- HDMI: the PC was not yet connected to the TV.
- Real SteamOS boot/shutdown/reboot, ten suspend/resume cycles and mode switching.
- Persistence across SteamOS updates and wake after many hours in standby.
- Public GitHub installation: source and first release have not been published.

Container and VM results do not replace these hardware checks. The automatic
onboarding and expanded logging also pass race-enabled tests, vet, Linux Docker
checks and the dedicated systemd service test. The actual TCL reports
manufacturer TCL, so its compatible power profile is selected automatically.
