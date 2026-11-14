#Requires -Version 7.0
[CmdletBinding()]
param([switch]$FrontendOnly, [switch]$InstallBrowser)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
$Root = Split-Path -Parent $PSScriptRoot
$Profile = if ($FrontendOnly) { 'Frontend' } else { 'Package' }
& (Join-Path $PSScriptRoot 'doctor.ps1') -Profile $Profile -ToolchainOnly
$Npm = Resolve-NoosphereCommand 'npm'
Invoke-NoosphereCommand $Npm @('ci', '--no-audit', '--no-fund') (Join-Path $Root 'frontend')
if ($InstallBrowser) {
  $BrowserCLI = Join-Path $Root 'frontend/node_modules/playwright/cli.js'
  Invoke-NoosphereCommand (Resolve-NoosphereCommand 'node') @($BrowserCLI, 'install', 'chromium') $Root
}
if (-not $FrontendOnly) {
  Invoke-NoosphereCommand $Npm @('ci', '--no-audit', '--no-fund') (Join-Path $Root 'desktop')
  $Venv = Join-Path $Root '.local/venv'
  $Python = Join-Path $Venv 'Scripts/python.exe'
  if (-not (Test-Path -LiteralPath $Python)) {
    Invoke-NoosphereCommand (Resolve-NoosphereCommand 'python') @('-m', 'venv', $Venv) $Root
  }
  $Requirements = Join-Path $Root 'scripts/codelens-runtime/requirements-win-py314.lock'
  Invoke-NoosphereCommand $Python @('-m', 'pip', 'install', '--disable-pip-version-check', '--only-binary=:all:', '--require-hashes', '-r', $Requirements) $Root
  Invoke-NoosphereCommand $Python @('-m', 'pip', 'check') $Root
}
& (Join-Path $PSScriptRoot 'doctor.ps1') -Profile $Profile
