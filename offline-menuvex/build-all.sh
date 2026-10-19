#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

echo "==> Removing old binaries and build artifacts..."
rm -rf dist local coffish-desktop shell/build/bin
mkdir -p dist

HOST_OS="$(uname -s 2>/dev/null || echo Unknown)"

echo "==> Building Coffish Desktop cross-platform executables..."

TARGETS=(
  "windows/amd64/.exe"
  "windows/arm64/.exe"
  "darwin/arm64/"
  "darwin/amd64/"
  "linux/amd64/"
  "linux/arm64/"
)

for item in "${TARGETS[@]}"; do
  IFS='/' read -r goos goarch ext <<< "$item"
  out="dist/coffish-desktop-${goos}-${goarch}${ext}"
  cgo_flag="0"
  if [[ "${goos}" == "darwin" && "${HOST_OS}" == "Darwin" ]]; then
    cgo_flag="1"
  fi
  echo "  -> Building ${out} (CGO_ENABLED=${cgo_flag}) ..."
  CGO_ENABLED="${cgo_flag}" GOOS="${goos}" GOARCH="${goarch}" go build -a -trimpath -ldflags="-s -w" -o "${out}" ./cmd/coffish-desktop
done

echo "==> Packaging native macOS .app bundle (dist/CoffishDesktop.app)..."
mkdir -p dist/CoffishDesktop.app/Contents/MacOS
mkdir -p dist/CoffishDesktop.app/Contents/Resources
cp dist/coffish-desktop-darwin-arm64 dist/CoffishDesktop.app/Contents/MacOS/CoffishDesktop
chmod +x dist/CoffishDesktop.app/Contents/MacOS/CoffishDesktop
cat > dist/CoffishDesktop.app/Contents/Info.plist <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleExecutable</key><string>CoffishDesktop</string>
  <key>CFBundleIdentifier</key><string>ir.coffish.desktop</string>
  <key>CFBundleName</key><string>Coffish Desktop</string>
  <key>CFBundleDisplayName</key><string>کافیش دسکتاپ</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>2.1.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
EOF

echo "==> Done! Fresh native desktop binaries created in desktop/dist/"
