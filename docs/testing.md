# Testing

## Automated checks

`make test` runs the race detector. Tests cover cancellation, repeated power
commands, wake order, bounded standby, unverified HDMI, input links and sequences,
persisted boot/toggle state, atomic configuration and file permissions.
Controller deadline and retry scenarios use Go's virtual test clock so slow
filesystem synchronization cannot expire a confirmation timer before key send.

A real local TLS server exercises mutual TLS, varint protobuf framing,
fragmentation, power reports, exact input-link delivery, TCL startup key loss
and cancellation of a silent pairing server. Closing the listener while leaving
the control socket open verifies standby without reconnecting, including a
30 ms reuse budget on TCL. Additional checks cover keepalive responses, power
invalidation after peer EOF, reconnection with a different state and resetting
the transport before the resume action.
A UDP receiver checks the Wake-on-LAN payload.

When dbus-daemon is available, tests create an isolated bus and a mock logind
service. Real signals and Unix FD passing verify inhibitor lifetime and
reacquisition after resume, including an older interface without shutdown metadata.

Onboarding tests cover automatic selection of a single TV, multiple devices,
TCL profile selection, skipped and verified HDMI, input-link fallback with
visual confirmation, and rejected pairing. They
assert that no TUI or pairing code appears in the output. Logging tests check
journald severity prefixes and omission of duplicate timestamps.

`make test-docker` runs tests, vet and ShellCheck. The installer runs as an
ordinary user with a read-only container root and writable home/tmp. systemctl,
loginctl and installer architecture selection use fixtures; the actual amd64
executable is also run through binfmt on Apple Silicon. Release checksum checking
stays enabled. Tests include the curl-to-bash pipeline, a home path with spaces,
updates, rollback, corrupt and missing downloads. A real PTY checks the advanced
TUI, Escape, quitting, and absence of partial configuration.

`make test-systemd` uses only the dedicated OrbStack machine below:

```bash
orb create --arch amd64 --isolated --mount "$PWD:/work" --user tvtest arch steamos-tv-control-test
```

The script provisions polkit in this test machine, then checks actual systemd
user service installation, linger, startup, restart, IPC, logind and removal.
TV automation is disabled in the fixture. The machine remains available for
inspection. Existing user machines are not modified.

## Hardware acceptance

1. Install on SteamOS while its system image remains read-only. Capture a report
   with bash scripts/acceptance.sh REPORT_DIRECTORY.
2. Pair the TV and verify HDMI from Home and another input.
3. Cold boot: the TV wakes and selects the PC input.
4. Run ten suspend/resume cycles. Record outcomes and image appearance times.
   The application must not hold its inhibitor beyond the configured budget;
   other system services may contribute their own delays.
5. Shutdown sends the TV to standby; reboot leaves it on.
6. Switch Desktop/Gaming Mode and restart the service: no additional power
   commands or HDMI takeover should occur.
7. Turn the TV off with its remote and restore network connectivity: it must
   remain off until a new PC power event or manual command.
8. Test delayed network recovery, revoked pairing and prolonged standby.
9. Verify persistence after a normal SteamOS update.

Distinguish command delivery, protocol power reports and visible screen behavior.
Docker and OrbStack do not prove actual SteamOS suspend behavior, HDMI switching,
LAN broadcast delivery or TV wake compatibility. Record untested cases separately.
Never include pairing codes, certificates or private keys in diagnostic reports.
