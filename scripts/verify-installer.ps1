#Requires -Version 7.0
[CmdletBinding()]
param([Parameter(Mandatory)][string]$Installer)

$ErrorActionPreference = 'Stop'
# Tauri NSIS registers a single app for the Windows user. The checks below
# reject an existing install or data.
. (Join-Path $PSScriptRoot 'common.ps1')
$Root = Split-Path -Parent $PSScriptRoot
$Installer = (Resolve-Path -LiteralPath $Installer).Path
$Fixture = Join-Path ([IO.Path]::GetTempPath()) ('noosphere-installer-' + [guid]::NewGuid().ToString('N'))
$Apps = @('HKCU:/Software/Microsoft/Windows/CurrentVersion/Uninstall',
          'HKLM:/Software/Microsoft/Windows/CurrentVersion/Uninstall',
          'HKLM:/Software/WOW6432Node/Microsoft/Windows/CurrentVersion/Uninstall')
function Get-RegisteredNoosphere {
  foreach ($App in $Apps) {
    if (Test-Path -LiteralPath $App) {
      Get-ItemProperty -Path "$App/*" -ErrorAction SilentlyContinue |
        Where-Object { $_.DisplayName -like 'Noosphere Desktop*' }
    }
  }
}
if (@(Get-RegisteredNoosphere).Count) { throw 'A Noosphere install is already registered on this runner.' }
$UserRoot = Join-Path $env:LOCALAPPDATA 'com.noosphere.desktop'
if (Test-Path -LiteralPath $UserRoot) { throw 'Existing Noosphere user data is present on this runner.' }
$InstallRoot = Join-Path $Fixture 'Custom Installation'
$Uninstaller = Join-Path $InstallRoot 'uninstall.exe'
$Marker = Join-Path $UserRoot 'installer-data-preservation.txt'
function Invoke-InstallerProcess {
  param([string]$Path, [string]$Arguments, [int]$ExpectedExit = 0)
  $Process = Start-Process -FilePath $Path -ArgumentList $Arguments -WindowStyle Hidden -Wait -PassThru
  if ($Process.ExitCode -ne $ExpectedExit) {
    throw "$Path exited with $($Process.ExitCode), expected $ExpectedExit."
  }
}
function Install-Fixture {
  Invoke-InstallerProcess $Installer "/S /D=$InstallRoot"
  if (-not (Test-Path -LiteralPath $Uninstaller -PathType Leaf)) { throw 'Uninstaller missing in selected path.' }
  $Registered = @(Get-RegisteredNoosphere)
  if ($Registered.Count -ne 1) { throw 'Windows uninstall entry is missing or duplicated.' }
  $Version = Get-NoosphereVersion $Root
  if ($Registered[0].DisplayVersion -ne $Version) { throw 'Installed version differs from VERSION.' }
}
function Uninstall-Fixture {
  # NSIS copies the uninstaller to a temporary location before removing the
  # registered directory. _?= runs in place, so use an owned copy and wait.
  $Copy = Join-Path $Fixture 'uninstall-fixture.exe'
  Copy-Item -LiteralPath $Uninstaller -Destination $Copy -Force
  Invoke-InstallerProcess $Copy "/S _?=$InstallRoot"
  if (@(Get-RegisteredNoosphere).Count) { throw 'Uninstall left a registered Noosphere entry.' }
  if (@(Get-ChildItem -LiteralPath $InstallRoot -Filter '*.exe' -File -ErrorAction SilentlyContinue |
      Where-Object Name -ne 'uninstall.exe').Count) { throw 'Uninstall left the desktop executable in the selected location.' }
}
Install-Fixture
$Desktop = @(Get-ChildItem -LiteralPath $InstallRoot -Filter '*.exe' -File |
  Where-Object Name -ne 'uninstall.exe')
if ($Desktop.Count -ne 1) { throw 'Expected one installed desktop executable.' }
Invoke-NoosphereCommand (Resolve-NoosphereCommand 'node') @(
  (Join-Path $PSScriptRoot 'verify-desktop.cjs'), $InstallRoot, $Desktop[0].Name
) $Root
$DesktopHash = (Get-FileHash -LiteralPath $Desktop[0].FullName).Hash
$Process = Start-Process -FilePath $Desktop[0].FullName -PassThru
try {
  $null = $Process.WaitForInputIdle(15000)
  Invoke-InstallerProcess $Installer "/S /D=$InstallRoot" -ExpectedExit 2
  if ($Process.HasExited -or (Get-FileHash -LiteralPath $Desktop[0].FullName).Hash -ne $DesktopHash) {
    throw 'The installer terminated or replaced the running app.'
  }
} finally {
  if (-not $Process.HasExited) {
    $null = $Process.CloseMainWindow()
    if (-not $Process.WaitForExit(15000)) { $Process.Kill($true); $Process.WaitForExit() }
  }
}
New-Item -ItemType Directory -Path $UserRoot -Force | Out-Null
[IO.File]::WriteAllText($Marker, 'retained across uninstall and reinstall')
Uninstall-Fixture
if (-not (Test-Path -LiteralPath $Marker)) { throw 'Uninstall deleted user data.' }
Install-Fixture
if ([IO.File]::ReadAllText($Marker) -ne 'retained across uninstall and reinstall') {
  throw 'Reinstall modified existing user data.'
}
Uninstall-Fixture
Write-Host "PASS selected NSIS path, installed desktop, version registration, active-process refusal, uninstall, reinstall and app-data preservation ($Fixture)"
