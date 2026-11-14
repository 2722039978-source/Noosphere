#Requires -Version 7.0
[CmdletBinding()]
param([switch]$Browser, [switch]$Rust, [string]$PackagedRoot)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
$Root = Split-Path -Parent $PSScriptRoot
$Frontend = Join-Path $Root 'frontend'
$Backend = Join-Path $Root 'backend'
$PythonSource = Join-Path $Backend 'python/codelens'
& (Join-Path $PSScriptRoot 'doctor.ps1') -Profile Desktop
$Node = Resolve-NoosphereCommand 'node'
$Go = Resolve-NoosphereCommand 'go'
$Python = Join-Path $Root '.local/venv/Scripts/python.exe'
$Temp = New-Item -ItemType Directory -Force -Path (Join-Path $Root 'test/cache/suite')
$Previous = @{ TEMP = $env:TEMP; TMP = $env:TMP; TMPDIR = $env:TMPDIR; GOTOOLCHAIN = $env:GOTOOLCHAIN }
try {
  $env:TEMP = $env:TMP = $env:TMPDIR = $Temp.FullName
  if (-not $env:GOTOOLCHAIN) { $env:GOTOOLCHAIN = 'local' }
  Invoke-NoosphereCommand (Resolve-NoosphereCommand 'npm') @('run', 'build') $Frontend
  $Tests = @(Get-ChildItem -LiteralPath (Join-Path $Frontend 'tests') -Filter '*.test.mjs' |
    ForEach-Object { $_.FullName })
  if (-not $Tests) { throw 'Frontend test files were not found.' }
  Invoke-NoosphereCommand $Node (@('--test') + $Tests) $Frontend
  # The WASI fixtures compile a fresh toolchain cache under a strict deadline.
  # Serialize test packages to avoid contention between independent compilers.
  Invoke-NoosphereCommand $Go @('test', '-p', '1', '-count=1', './...') $Backend
  Invoke-NoosphereCommand $Go @('vet', './...') $Backend
  Invoke-NoosphereCommand $Go @('build', './...') $Backend
  Invoke-NoosphereCommand $Python @('-B', (Join-Path $PSScriptRoot 'verify-grammars.py')) $PythonSource
  if ($Browser) { Invoke-NoosphereCommand $Node @((Join-Path $PSScriptRoot 'verify-browser.cjs')) $Frontend }
  if ($Rust) {
    $Cargo = Resolve-NoosphereCommand 'cargo'
    Invoke-NoosphereCommand $Cargo @('test', '--locked', '--lib') (Join-Path $Root 'desktop/src-tauri')
  }
  if ($PackagedRoot) {
    Invoke-NoosphereCommand $Node @((Join-Path $PSScriptRoot 'verify-desktop.cjs'), [IO.Path]::GetFullPath($PackagedRoot)) $Root
  }
} finally {
  $env:TEMP = $Previous.TEMP
  $env:TMP = $Previous.TMP
  $env:TMPDIR = $Previous.TMPDIR
  $env:GOTOOLCHAIN = $Previous.GOTOOLCHAIN
}
