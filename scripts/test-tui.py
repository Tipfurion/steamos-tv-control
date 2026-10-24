#!/usr/bin/env python3
"""Exercise a real Bubble Tea session, including terminal input and cancellation."""
import os
import fcntl
import pty
import select
import signal
import sys
import struct
import termios
import tempfile
import time
from pathlib import Path

with tempfile.TemporaryDirectory(prefix="tv-tui-") as work:
    pid, master = pty.fork()
    if pid == 0:
        env = dict(os.environ, TERM="xterm-256color", XDG_CONFIG_HOME=work,
                   XDG_DATA_HOME=work, XDG_STATE_HOME=work)
        os.execve(sys.argv[1], [sys.argv[1], "setup"], env)
    output = b""
    fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    escaped = False
    quit_sent = False
    reaped = False
    try:
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            readable, _, _ = select.select([master], [], [], 0.1)
            if readable:
                try:
                    chunk = os.read(master, 65536)
                except OSError:
                    break
                output += chunk
                if b"\x1b[c" in chunk:
                    os.write(master, b"\x1b[?1;2c")
                if "TV IP address or hostname".encode() in output and not escaped:
                    os.write(master, b"\x1b")
                    escaped = True
                    output = b""
                elif "Pair".encode() in output and escaped and not quit_sent:
                    os.write(master, b"q")
                    quit_sent = True
            ended, status = os.waitpid(pid, os.WNOHANG)
            if ended:
                reaped = True
                assert os.waitstatus_to_exitcode(status) == 0, output.decode(errors="replace")
                break
        if not reaped:
            ended, status = os.waitpid(pid, os.WNOHANG)
            reaped = bool(ended)
        assert escaped and quit_sent, output.decode(errors="replace")
        assert reaped, "TUI did not exit"
        assert not (Path(work) / "steamos-tv-control/config.json").exists()
        print("PASS: real TUI in PTY, setup, Escape, menu, quit, no partial config")
    finally:
        os.close(master)
        if not reaped:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
