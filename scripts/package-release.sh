#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
version=${1:-}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][a-zA-Z0-9.-]+)?$ ]] || {
    echo 'Use a release version such as v0.1.0 or v0.1.0-rc.1.' >&2
    exit 1
}
mkdir -p dist/package
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "${GO:-go}" build -trimpath \
    -ldflags "-s -w -X github.com/Tipfurion/steamos-tv-control/internal/app.Version=$version" \
    -o dist/package/steamos-tv-control ./cmd/steamos-tv-control
COPYFILE_DISABLE=1 tar --no-xattrs -C dist/package -czf dist/steamos-tv-control_linux_amd64.tar.gz steamos-tv-control
# Replace the assignment only; the sentinel comparison must stay untouched.
sed "s/^release_version='@RELEASE_VERSION@'$/release_version='$version'/" install.sh > dist/install.sh
chmod 755 dist/install.sh
if command -v sha256sum >/dev/null; then
    (cd dist; sha256sum install.sh steamos-tv-control_linux_amd64.tar.gz > SHA256SUMS)
else
    (cd dist; shasum -a 256 install.sh steamos-tv-control_linux_amd64.tar.gz > SHA256SUMS)
fi
notes="docs/releases/$version.md"
if [ -f "$notes" ]; then
    cat "$notes" > dist/release-notes.md
    printf '\n\n' >> dist/release-notes.md
else
    : > dist/release-notes.md
fi
cat >> dist/release-notes.md <<NOTES
## Install $version

Switch SteamOS to Desktop Mode, open Konsole, and run:

\`\`\`bash
curl -fsSL https://github.com/Tipfurion/steamos-tv-control/releases/download/$version/install.sh | bash
\`\`\`

This installs $version, including automatic TV discovery and terminal pairing.

[English instructions](https://github.com/Tipfurion/steamos-tv-control/blob/$version/README.md) · [Russian instructions](https://github.com/Tipfurion/steamos-tv-control/blob/$version/README.ru.md)
NOTES
