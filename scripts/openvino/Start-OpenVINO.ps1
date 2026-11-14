[CmdletBinding()]
param(
  [ValidateSet('GPU','GPU.0','CPU')][string]$Device = 'GPU',
  [ValidateRange(1,16)][int]$CPUThreads = 2,
  [ValidateRange(512,8192)][int]$ContextTokens = 4096,
  [ValidateRange(1024,65535)][int]$Port = 18789,
  [string]$RuntimeHome = (Join-Path (Split-Path (Split-Path $PSScriptRoot -Parent) -Parent) 'model/OpenVINO')
)
$ErrorActionPreference = 'Stop'
$RuntimeHome = [IO.Path]::GetFullPath($RuntimeHome)
$Python = Join-Path $RuntimeHome 'venv/Scripts/python.exe'
if (-not (Test-Path -LiteralPath $Python)) { throw 'Run Install-OpenVINO.ps1 first.' }
& $Python (Join-Path $PSScriptRoot 'launcher.py') start --runtime-home $RuntimeHome --device $Device --cpu-threads $CPUThreads --num-ctx $ContextTokens --port $Port
exit $LASTEXITCODE
