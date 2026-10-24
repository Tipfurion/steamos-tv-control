#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
docker build -f scripts/test.Dockerfile -t steamos-tv-control-test .
docker run --rm --read-only --tmpfs /tmp:rw,exec,mode=1777 --tmpfs /home/tester:rw,exec,uid=1000,gid=1000 --security-opt no-new-privileges steamos-tv-control-test
# The entrypoint is an actual amd64 executable (built with GOARCH=amd64).
# OrbStack's binfmt/Rosetta runs it on Apple Silicon without requesting an
# unrelated amd64 manifest for the locally built ARM test-harness image.
docker run --rm --entrypoint /tmp/steamos-tv-control steamos-tv-control-test version
