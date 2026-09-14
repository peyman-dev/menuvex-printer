# Install the Novex Printer Agent on Windows (user-level, no admin needed).
#
#   powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1 [-Bin path\to\binary.exe]
param(
  [string]$Bin = ""
)

$ErrorActionPreference = "Stop"
$Dest = Join-Path $env:LOCALAPPDATA "NovexPrinterAgent"
$Exe = Join-Path $Dest "NovexPrinterAgent.exe"

if ($Bin -eq "") {
  $repoRoot = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
  $candidates = @(
    (Join-Path $repoRoot "dist\novex-printer-agent-windows-amd64.exe"),
    (Join-Path $repoRoot "novex-printer-agent.exe")
  )
  foreach ($c in $candidates) { if (Test-Path $c) { $Bin = $c; break } }
  if ($Bin -eq "") {
    if (Get-Command go -ErrorAction SilentlyContinue) {
      Write-Host "Building..."
      Push-Location $repoRoot
      $env:CGO_ENABLED = "0"
      go build -trimpath -o "$env:TEMP\NovexPrinterAgent.exe" .\cmd\agent
      Pop-Location
      $Bin = "$env:TEMP\NovexPrinterAgent.exe"
    } else {
      throw "No binary found. Build with Go or pass -Bin <path>."
    }
  }
}

New-Item -ItemType Directory -Force -Path $Dest | Out-Null
Copy-Item $Bin $Exe -Force
Write-Host "Installed: $Exe"

# Start-on-login (writes a hidden launcher into the Startup folder).
& $Exe --install-autostart

Write-Host ""
Write-Host "USB note: install the printer in Windows Settings > Printers first"
Write-Host "(any driver works — the agent uses RAW passthrough). See docs/USB.md."
Write-Host ""
Write-Host ("Token: " + (& $Exe --print-token))
Write-Host "Run:   $Exe"
