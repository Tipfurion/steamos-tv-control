#!/usr/bin/env python3
"""Check release coherence without executing the Linux binary on macOS."""
import hashlib
import os
import platform
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile

dist = Path("dist")
script = dist / "install.sh"
text = script.read_text()
match = re.search(r"^release_version='(v[^']+)'$", text, re.MULTILINE)
assert match, "Release installer does not embed a version"
version = match.group(1)
assert "releases/latest" not in text, "Pinned installer resolves a floating release"
names = set()
for line in (dist / "SHA256SUMS").read_text().splitlines():
    digest, name = line.split()
    assert name in {"install.sh", "steamos-tv-control_linux_amd64.tar.gz"}
    assert hashlib.sha256((dist / name).read_bytes()).hexdigest() == digest
    names.add(name)
assert names == {"install.sh", "steamos-tv-control_linux_amd64.tar.gz"}
with tarfile.open(dist / "steamos-tv-control_linux_amd64.tar.gz") as archive:
    assert archive.getnames() == ["steamos-tv-control"]
    executable = archive.extractfile("steamos-tv-control").read()
    assert executable[:4] == b"\x7fELF" and executable[18:20] == b"\x3e\x00"
assert version.encode() in executable, "Binary does not contain the release version"
if sys.platform == "linux" and platform.machine() in {"x86_64", "amd64"}:
    actual = subprocess.check_output([str(dist / "package/steamos-tv-control"), "version"], text=True).strip()
    assert actual == version, actual
assert f"releases/download/{version}/install.sh" in (dist / "release-notes.md").read_text()

# A published installer must reject a different version before any download.
with tempfile.TemporaryDirectory(prefix="tv-release-") as work:
    work = Path(work)
    commands = {
        "id": "echo 1000",
        "uname": 'case "$1" in -s) echo Linux ;; -m) echo x86_64 ;; esac',
        "systemctl": "exit 0",
        "loginctl": "exit 0",
        "sha256sum": "exit 99",
        "curl": 'printf "%s\\n" "$*" >> "$TEST_DOWNLOAD_LOG"; exit 81',
    }
    for name, body in commands.items():
        command = work / name
        command.write_text("#!/bin/sh\n" + body + "\n")
        command.chmod(0o755)
    env = dict(os.environ, PATH=str(work) + os.pathsep + os.environ["PATH"],
               STEAMOS_TV_CONTROL_VERSION="v999.0.0", TEST_DOWNLOAD_LOG=str(work / "downloads"))
    result = subprocess.run(["bash", str(script)], env=env, capture_output=True, text=True)
    assert result.returncode and "belongs to " + version in result.stderr
    assert not (work / "downloads").exists()
    env.pop("STEAMOS_TV_CONTROL_VERSION")
    result = subprocess.run(["bash", str(script)], env=env, capture_output=True, text=True)
    assert result.returncode == 81, result.stderr
    assert f"releases/download/{version}/steamos-tv-control_linux_amd64.tar.gz" in (work / "downloads").read_text()
print(f"PASS: {version} installer, binary, archive, checksums, version pin and override rejection")
