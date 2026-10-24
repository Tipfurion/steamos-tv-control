#!/usr/bin/env bash
set -euo pipefail
binary=${STEAMOS_TV_CONTROL_BINARY:-$HOME/.local/bin/steamos-tv-control}
output=${1:-steamos-tv-control-report}
mkdir -p "$output"
"$binary" doctor --json > "$output/doctor.json"
"$binary" status --json > "$output/status.json"
systemctl --version > "$output/systemd.txt"
findmnt --target / > "$output/mounts.txt"
findmnt --target /home >> "$output/mounts.txt"
journalctl --user -u steamos-tv-control -b --no-pager > "$output/journal.txt"
printf 'Diagnostics saved to %s. Certificates and keys are excluded.\n' "$output"
printf 'Hardware acceptance instructions: docs/testing.md\n'
