param([switch]$InstallBuildTools)
$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
function Run-Native([string]$Exe, [string[]]$Arguments) {
  $PreviousPreference = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  $Output = & $Exe @Arguments 2>&1
  $ExitCode = $LASTEXITCODE
  $ErrorActionPreference = $PreviousPreference
  $Output | ForEach-Object { Write-Host $_ }
  if ($ExitCode -ne 0) {
    $Errors = ($Output | Where-Object { "$_" -match '(?i)error|failed|exception' } | Select-Object -First 8) -join ' | '
    if ($env:GITHUB_ACTIONS -eq 'true') {
      $Escaped = $Errors.Replace('%','%25').Replace("`r",'%0D').Replace("`n",'%0A')
      Write-Host "::error::$Exe failed: $Escaped"
    }
    throw "$Exe failed with exit code $ExitCode"
  }
}
$Nsis = Join-Path ${env:ProgramFiles(x86)} 'NSIS\makensis.exe'
if (!(Test-Path $Nsis)) {
  if (!$InstallBuildTools) { throw 'Install NSIS on this build machine first (not on customer computers).' }
  Run-Native 'choco' @('install','nsis','--yes','--no-progress')
}
$VsWhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
$VsPath = & $VsWhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
if (!$VsPath) { throw 'Visual Studio C++ x86/x64 build tools required' }
$Dumpbin = Get-ChildItem (Join-Path $VsPath 'VC\Tools\MSVC\*\bin\Hostx64\x64\dumpbin.exe') | Sort-Object FullName -Descending | Select-Object -First 1
if (!$Dumpbin) { throw 'dumpbin.exe not found' }
foreach ($Arch in @('x86','x64')) {
  $Platform = if ($Arch -eq 'x86') { 'Win32' } else { 'x64' }
  $Build = Join-Path $Root "target\legacy-$Arch"
  Run-Native 'cmake' @('-S', (Join-Path $Root 'legacy-windows'), '-B', $Build, '-G','Visual Studio 17 2022','-A',$Platform)
  Run-Native 'cmake' @('--build',$Build,'--config','Release','--parallel','2')
  Run-Native 'ctest' @('--test-dir',$Build,'-C','Release','--output-on-failure')
  $Binary = Join-Path $Build 'Release\menuvex-printer-legacy.exe'
  $Output = Join-Path $Root "dist\legacy-installers\$Arch"
  New-Item -ItemType Directory -Force -Path $Output | Out-Null
  $Imports = & $Dumpbin.FullName /imports $Binary
  if ($LASTEXITCODE -ne 0) { throw 'PE import audit failed to execute' }
  $Imports | Set-Content (Join-Path $Output 'imports-audit.txt')
  # A denylist is only a static regression guard, not a complete Win7 compatibility proof.
  $Forbidden = 'PackageIdFromFullName|GetCurrentPackageId|GetSystemTimePreciseAsFileTime|WaitOnAddress|WakeByAddressSingle|WakeByAddressAll|GetOverlappedResultEx|GetCurrentThreadStackLimits|SetThreadDescription|GetDpiForWindow|SetProcessDpiAwarenessContext|api-ms-win-|VCRUNTIME\d+\.dll|MSVCP\d+\.dll|WebView2Loader'
  if (($Imports -join "`n") -match $Forbidden) { throw "Potential Windows 7-incompatible import in $Arch binary. Review imports-audit.txt before changing toolchain." }
  $Licenses = @(
    (Join-Path $Build '_deps\json-src\LICENSE.MIT'),
    (Join-Path $Build '_deps\websocketpp-src\COPYING'),
    (Join-Path $Build '_deps\asio-src\asio\LICENSE_1_0.txt'),
    (Join-Path $Root 'src-tauri\assets\OFL-NotoSansArabic.txt')
  )
  $LicenseText = ''
  foreach ($File in $Licenses) { if (!(Test-Path $File)) { throw "Missing third-party license: $File" }; $LicenseText += "`r`n--- $([IO.Path]::GetFileName($File)) ---`r`n" + (Get-Content $File -Raw) }
  $LicenseOut = Join-Path $Output 'THIRD-PARTY-LICENSES.txt'
  $LicenseText | Set-Content -Encoding UTF8 $LicenseOut
  $Installer = Join-Path $Output "MenuVex-Printer-Agent-Legacy-1.0.0-windows7-$Arch-setup.exe"
  Run-Native $Nsis @("/DARCH=$Arch", "/DBINARY=$Binary", "/DOUTPUT=$Installer", "/DLICENSES=$LicenseOut", (Join-Path $Root 'legacy-windows\installer\setup.nsi'))
  $Hash = (Get-FileHash $Installer -Algorithm SHA256).Hash.ToLowerInvariant()
  "$Hash  $([IO.Path]::GetFileName($Installer))" | Set-Content -Encoding ASCII (Join-Path $Output 'SHA256SUMS.txt')
  @{ version='1.0.0'; platform='windows'; architecture=$Arch; variant='legacy'; minimumOs='Windows 7 SP1'; channel='test-candidate'; win7HardwareValidated=$false; signed=$false; filename=[IO.Path]::GetFileName($Installer); sha256=$Hash; bytes=(Get-Item $Installer).Length } | ConvertTo-Json | Set-Content -Encoding UTF8 (Join-Path $Output 'manifest.json')
}
