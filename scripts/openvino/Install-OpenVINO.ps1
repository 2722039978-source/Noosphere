[CmdletBinding()]
param(
  [string]$Python = 'py',
  [string]$RuntimeHome = (Join-Path (Split-Path (Split-Path $PSScriptRoot -Parent) -Parent) 'model/OpenVINO'),
  [switch]$Mirror,
  [switch]$Direct
)
$ErrorActionPreference = 'Stop'
$RuntimeHome = [IO.Path]::GetFullPath($RuntimeHome)
$ExistingPython = Join-Path $RuntimeHome 'venv/Scripts/python.exe'
if (Test-Path -LiteralPath $ExistingPython) {
  $StatusText = & $ExistingPython (Join-Path $PSScriptRoot 'launcher.py') status --runtime-home $RuntimeHome
  if ($LASTEXITCODE -ne 0) { throw 'Could not inspect the existing deployment; installation was not changed.' }
  if (($StatusText | ConvertFrom-Json).owned_state) { throw 'Stop the owned OpenVINO service before updating its environment.' }
}
$PythonCommand = (Get-Command $Python -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$PythonPrefix = @()
if ([IO.Path]::GetFileNameWithoutExtension($PythonCommand) -eq 'py') { $PythonPrefix = @('-3.14') }
$Probe = & $PythonCommand @PythonPrefix -c 'import struct,sys; assert sys.version_info[:2]==(3,14) and struct.calcsize("P")==8, "This hash-locked installer requires CPython 3.14 x64"; print(sys.executable)'
if ($LASTEXITCODE -ne 0) { throw 'CPython 3.14 x64 is required by this verified Windows dependency lock.' }
New-Item -ItemType Directory -Force -Path $RuntimeHome,(Join-Path $RuntimeHome 'logs') | Out-Null
$Venv = Join-Path $RuntimeHome 'venv'
& $PythonCommand @PythonPrefix -m venv $Venv
if ($LASTEXITCODE -ne 0) { throw 'Creating the isolated environment failed.' }
$VenvPython = Join-Path $Venv 'Scripts/python.exe'
& $VenvPython -m pip install --disable-pip-version-check --no-cache-dir --only-binary=:all: --require-hashes -r (Join-Path $PSScriptRoot 'requirements-win-py314.lock') --report (Join-Path $RuntimeHome 'logs/pip-install-report.json')
if ($LASTEXITCODE -ne 0) { throw 'The pinned OpenVINO installation failed.' }
$Download = @((Join-Path $PSScriptRoot 'download_model.py'), '--target', (Join-Path $RuntimeHome 'models/Qwen3-0.6B-int4-ov'), '--report', (Join-Path $RuntimeHome 'logs/download-report.json'))
if ($Mirror) { $Download += '--mirror' }
if ($Direct) { $Download += '--direct' }
& $VenvPython @Download
if ($LASTEXITCODE -ne 0) { throw 'Model download or identity verification failed. Partial files are retained for resume.' }
Write-Host 'OpenVINO installed and weights verified. No model service was started.'
Write-Host 'Use Start-OpenVINO.ps1 -Device GPU or -Device CPU, then select the local OpenVINO preset in Model Hub.'
