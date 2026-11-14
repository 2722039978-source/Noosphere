param([string]$GoBinary = 'go')

$ErrorActionPreference = 'Stop'
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$BackendRoot = Join-Path $ProjectRoot 'backend'
$TemporaryRoot = Join-Path $ProjectRoot 'test/cache/core-tests'
New-Item -ItemType Directory -Path $TemporaryRoot -Force | Out-Null
$PreviousTemp = $env:TEMP
$PreviousTmp = $env:TMP
$PreviousTmpDir = $env:TMPDIR
$Packages = @('./internal/evalrunner', './internal/gateway', './internal/modelstore', './internal/modellab')

try {
  $env:TEMP = $TemporaryRoot
  $env:TMP = $TemporaryRoot
  $env:TMPDIR = $TemporaryRoot
  foreach ($Package in $Packages) {
    $Tests = & $GoBinary -C $BackendRoot test -list '^TestPublic' $Package
    if ($LASTEXITCODE -ne 0) { throw "测试清单读取失败：$Package" }
    if (-not ($Tests | Where-Object { $_ -match '^TestPublic' })) { throw "核心测试缺失：$Package" }
  }
  & $GoBinary -C $BackendRoot test -count=1 -timeout 2m -run '^TestPublic' @Packages
  if ($LASTEXITCODE -ne 0) { throw '核心测试失败。' }
} finally {
  $env:TEMP = $PreviousTemp
  $env:TMP = $PreviousTmp
  $env:TMPDIR = $PreviousTmpDir
}
