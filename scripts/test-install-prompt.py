#!/usr/bin/env python3
"""Verify first installation uses terminal prompts through curl | bash, not TUI."""
import os
import errno
import pty
import select
import signal
import sys
import time
from pathlib import Path

home = sys.argv[1]
pid, master = pty.fork()
if pid == 0:
    env = dict(os.environ, HOME=home)
    env.pop("STEAMOS_TV_CONTROL_NO_SETUP", None)
    os.execvpe("bash", ["bash", "-c", 'curl -fsSL "file://$PWD/install.sh" | bash'], env)
output = b""
sent = False
reaped = False
try:
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        readable, _, _ = select.select([master], [], [], 0.1)
        if readable:
            try:
                chunk = os.read(master, 65536)
            except OSError as error:
                if error.errno != errno.EIO:
                    raise
                break
            if not chunk:
                break
            output += chunk
        if b"PAIRING_CODE:" in output and not sent:
            os.write(master, b"fixture-code\n")
            sent = True
        ended, status = os.waitpid(pid, os.WNOHANG)
        if ended:
            reaped = True
            break
    # The shell can close the PTY just before its exit becomes waitable.
    while not reaped and time.monotonic() < deadline:
        ended, status = os.waitpid(pid, os.WNOHANG)
        reaped = bool(ended)
        if not reaped:
            time.sleep(0.01)
    assert sent and reaped, output.decode(errors="replace")
    assert os.waitstatus_to_exitcode(status) == 0, output.decode(errors="replace")
    assert (Path(home) / ".config/steamos-tv-control/config.json").exists()
    assert b"\x1b[?1049h" not in output, "Installer opened a full-screen wizard"
    print("PASS: first install, curl pipeline, pairing prompt, no setup wizard")
finally:
    os.close(master)
    if not reaped:
        os.kill(pid, signal.SIGKILL)
        os.waitpid(pid, 0)
