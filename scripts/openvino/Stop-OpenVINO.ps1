[CmdletBinding()]
param(
  [ValidateRange(1024,65535)][int]$Port = 18789,
  [string]$RuntimeHome = (Join-Path (Split-Path (Split-Path $PSScriptRoot -Parent) -Parent) 'model/OpenVINO')
)
$ErrorActionPreference = 'Stop'
$RuntimeHome = [IO.Path]::GetFullPath($RuntimeHome)
$Python = Join-Path $RuntimeHome 'venv/Scripts/python.exe'
if (-not (Test-Path -LiteralPath $Python)) { throw 'No isolated OpenVINO Python was found; no process was stopped.' }
& $Python (Join-Path $PSScriptRoot 'launcher.py') stop --runtime-home $RuntimeHome --port $Port
exit $LASTEXITCODE
