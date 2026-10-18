# Read-only development environment check; does not install tools or call models.
param([switch]$RequireDesktop)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
$ProjectRoot=Split-Path -Parent $PSScriptRoot
$Failures=@()
foreach($Name in @('node','npm','go','python','cargo')) {
  try {
    $Command=Resolve-NoosphereCommand $Name
    $Version=& $Command --version 2>&1
    if($Name -eq 'go') { $Version=& $Command version }
    if($LASTEXITCODE -ne 0){throw "$Name version check failed"}
    Write-Host "$Name : $Command : $Version"
  } catch {
    if($Name -ne 'cargo' -or $RequireDesktop){$Failures+=$_.Exception.Message}
    else {Write-Warning $_.Exception.Message}
  }
}
$LocalPython=Join-Path $ProjectRoot '.local/venv/Scripts/python.exe'
if(Test-Path -LiteralPath $LocalPython){$PythonCommand=$LocalPython}else{$PythonCommand=Resolve-NoosphereCommand 'python'}
& $PythonCommand -c 'import fastapi, uvicorn, tree_sitter, tree_sitter_go, tree_sitter_python, tree_sitter_javascript, tree_sitter_typescript, tree_sitter_java; print("CodeLens grammar imports OK")'
if($LASTEXITCODE -ne 0){$Failures+='CodeLens dependencies missing: install backend/python/codelens/requirements.txt'}
foreach($Directory in @('frontend','desktop')) {
 if(!(Test-Path -LiteralPath (Join-Path $ProjectRoot "$Directory/node_modules"))){$Failures+="Run npm --prefix $Directory ci"}
}
if($Failures.Count){throw ($Failures -join "`n")}
Write-Host 'Preflight passed (compiler/linker and application runtime are verified separately).'
