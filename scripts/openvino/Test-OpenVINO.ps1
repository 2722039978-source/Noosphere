[CmdletBinding()]
param(
  [ValidateSet('status','smoke')][string]$Action = 'status',
  [ValidateRange(1024,65535)][int]$Port = 18789,
  [string]$RuntimeHome = (Join-Path (Split-Path (Split-Path $PSScriptRoot -Parent) -Parent) 'model/OpenVINO')
)
$ErrorActionPreference = 'Stop'
$RuntimeHome = [IO.Path]::GetFullPath($RuntimeHome)
$Python = Join-Path $RuntimeHome 'venv/Scripts/python.exe'
if (-not (Test-Path -LiteralPath $Python)) { throw 'Run Install-OpenVINO.ps1 first.' }
& $Python (Join-Path $PSScriptRoot 'launcher.py') $Action --runtime-home $RuntimeHome --port $Port
exit $LASTEXITCODE
