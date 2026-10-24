#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
bundle="dist/SteamOS TV Control.app"
mkdir -p "$bundle/Contents/MacOS"
go build -o "$bundle/Contents/MacOS/steamos-tv-control" ./cmd/steamos-tv-control
cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>dev.tipfurion.steamos-tv-control</string>
<key>CFBundleExecutable</key><string>steamos-tv-control</string>
<key>CFBundleName</key><string>SteamOS TV Control</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleVersion</key><string>1</string>
<key>NSLocalNetworkUsageDescription</key><string>Find and control your Google TV for local hardware testing.</string>
<key>NSBonjourServices</key><array><string>_androidtvremote2._tcp</string></array>
</dict></plist>
PLIST
codesign --force --sign - "$bundle"
printf 'Run in a terminal: "%s/Contents/MacOS/steamos-tv-control" discover\n' "$bundle"
