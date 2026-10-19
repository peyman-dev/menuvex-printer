$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (Test-Path "dist") {
    Remove-Item -Recurse -Force "dist"
}
if (Test-Path "coffish-desktop.exe") {
    Remove-Item -Force "coffish-desktop.exe"
}
New-Item -ItemType Directory -Force -Path "dist" | Out-Null

$targets = @(
    @{ GOOS = "windows"; GOARCH = "amd64"; EXT = ".exe" },
    @{ GOOS = "windows"; GOARCH = "arm64"; EXT = ".exe" },
    @{ GOOS = "darwin";  GOARCH = "arm64"; EXT = "" },
    @{ GOOS = "darwin";  GOARCH = "amd64"; EXT = "" },
    @{ GOOS = "linux";   GOARCH = "amd64"; EXT = "" },
    @{ GOOS = "linux";   GOARCH = "arm64"; EXT = "" }
)

foreach ($t in $targets) {
    $env:CGO_ENABLED = "0"
    $env:GOOS = $t.GOOS
    $env:GOARCH = $t.GOARCH
    $out = "dist/coffish-desktop-$($t.GOOS)-$($t.GOARCH)$($t.EXT)"
    Write-Host "==> Building $out ..."
    go build -a -trimpath -ldflags="-s -w" -o $out ./cmd/coffish-desktop
}
Write-Host "==> All cross-OS binaries built in desktop/dist/"
