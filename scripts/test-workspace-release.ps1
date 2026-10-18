# Runs the packaged backend and Python worker against temporary data only.
# No live model, saved model configuration, or user project is used.
param([string]$PythonPath)
$ErrorActionPreference = 'Stop'
$ProjectRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$ReleaseRoot = Join-Path $ProjectRoot 'release'
$Executable = Join-Path $ReleaseRoot 'bin/noosphere-backend.exe'
if (-not $PythonPath) { $PythonPath = Join-Path $ProjectRoot '.local/venv/Scripts/python.exe' }
$FixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ('noosphere-release-workspaces-' + [guid]::NewGuid().ToString('N'))
$SourceRoot = Join-Path $FixtureRoot 'project'
New-Item -ItemType Directory -Path $SourceRoot -Force | Out-Null
[IO.File]::WriteAllText((Join-Path $SourceRoot 'example.py'), "def greet():`n    return 'workspace fixture'`n", [Text.UTF8Encoding]::new($false))
[IO.File]::WriteAllText((Join-Path $FixtureRoot 'models.yaml'), 'default_model: ""' + "`nproviders: []`n")
$BackendListener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$WorkerListener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$BackendListener.Start(); $WorkerListener.Start()
$BackendPort = $BackendListener.LocalEndpoint.Port; $WorkerPort = $WorkerListener.LocalEndpoint.Port
$BackendListener.Stop(); $WorkerListener.Stop()
$Config = @{
  app = @{ data_dir = (Join-Path $FixtureRoot 'data') }
  server = @{ host = '127.0.0.1'; port = $BackendPort }
  modules = @{
    nebula = @{ enabled = $true; data_dir = (Join-Path $FixtureRoot 'data/nebula'); embedding = @{ provider = 'mock' } }
    codelens = @{ enabled = $true; python_path = $PythonPath; port = $WorkerPort }
    devops = @{ enabled = $false }
  }
  gateway = @{ config_path = (Join-Path $FixtureRoot 'models.yaml') }
  logging = @{ dir = (Join-Path $FixtureRoot 'logs') }
}
$ConfigPath = Join-Path $FixtureRoot 'config.yaml'
[IO.File]::WriteAllText($ConfigPath, ($Config | ConvertTo-Json -Depth 8), [Text.UTF8Encoding]::new($false))
$PreviousToken = $env:NOOSPHERE_BACKEND_TOKEN
$PreviousScript = $env:NOOSPHERE_CODELENS_SCRIPT
$env:NOOSPHERE_BACKEND_TOKEN = [guid]::NewGuid().ToString('N')
$env:NOOSPHERE_CODELENS_SCRIPT = Join-Path $ReleaseRoot 'codelens-python/main.py'
$Headers = @{ Authorization = 'Bearer ' + $env:NOOSPHERE_BACKEND_TOKEN }
$ApiBase = "http://127.0.0.1:$BackendPort"
$script:BackendProcess = $null
$script:Launch = 0
function Start-FixtureBackend {
  $script:Launch++
  $script:BackendProcess = Start-Process -FilePath $Executable -ArgumentList @('-config', ('"' + $ConfigPath + '"')) -WorkingDirectory $FixtureRoot -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $FixtureRoot "stderr-$script:Launch.log") -RedirectStandardOutput (Join-Path $FixtureRoot "stdout-$script:Launch.log")
  $Deadline = (Get-Date).AddSeconds(25)
  while ((Get-Date) -lt $Deadline) {
    if ($script:BackendProcess.HasExited) { throw 'Packaged backend exited before healthy.' }
    try {
      $Health = Invoke-RestMethod "$ApiBase/health" -Headers $Headers -TimeoutSec 2
      if ($Health.modules.codelens.state -eq 'running' -and $Health.modules.workspaces.state -eq 'running') { return }
    } catch { }
    Start-Sleep -Milliseconds 150
  }
  throw 'Packaged workspace/CodeLens startup timed out.'
}
function Stop-FixtureBackend {
  if ($script:BackendProcess -and -not $script:BackendProcess.HasExited) {
    # Only stop child processes of the temporary backend owned by this test.
    $Children = @(Get-CimInstance Win32_Process -Filter "ParentProcessId = $($script:BackendProcess.Id)")
    foreach ($Child in $Children) { Stop-Process -Id $Child.ProcessId -ErrorAction SilentlyContinue }
    $script:BackendProcess.Kill(); $script:BackendProcess.WaitForExit()
  }
}
try {
  Start-FixtureBackend
  $Workspace = Invoke-RestMethod "$ApiBase/api/workspaces" -Headers $Headers -Method Post -ContentType 'application/json' -Body (@{name='Release fixture';project_path=$SourceRoot;default_model_id=''} | ConvertTo-Json)
  $Run = Invoke-RestMethod "$ApiBase/api/workspaces/$($Workspace.id)/runs" -Headers $Headers -Method Post -ContentType 'application/json' -Body '{"kind":"index","title":"Release index","input":{}}'
  $Deadline = (Get-Date).AddSeconds(20)
  do {
    $Result = Invoke-RestMethod "$ApiBase/api/runs/$($Run.id)" -Headers $Headers
    if ($Result.status -notin @('queued','running')) { break }
    Start-Sleep -Milliseconds 100
  } while ((Get-Date) -lt $Deadline)
  if ($Result.status -ne 'completed' -or $Result.result.retrieval_ready -ne $true -or $Result.result.total_files -ne 1) { throw "Packaged index failed: $($Result.status) $($Result.error)" }
  $Workspaces = Invoke-RestMethod "$ApiBase/api/workspaces" -Headers $Headers
  $Saved = $Workspaces.workspaces | Where-Object id -eq $Workspace.id
  if ($Saved.project_id -ne $Result.result.project_id) { throw 'Index binding was not saved.' }
  $Events = Invoke-RestMethod "$ApiBase/api/runs/$($Run.id)/events?after=0" -Headers $Headers
  if ($Events.events.Count -lt 4) { throw 'Progress events were not recorded.' }
  $Runs = Invoke-RestMethod "$ApiBase/api/workspaces/$($Workspace.id)/runs" -Headers $Headers
  if ($Runs.runs.Count -ne 1 -or $null -ne $Runs.runs[0].result) { throw 'Task list must expose lightweight metadata.' }
  if ($Result.result.chunk_strategy -ne 'symbols-v1' -or $Result.result.revision.Length -ne 64) { throw 'Packaged symbol index missing version.' }
  $Repeat = Invoke-RestMethod "$ApiBase/api/workspaces/$($Workspace.id)/runs" -Headers $Headers -Method Post -ContentType 'application/json' -Body '{"kind":"index","title":"Unchanged repeat","input":{}}'
  $Deadline = (Get-Date).AddSeconds(20)
  do {
    $Repeated = Invoke-RestMethod "$ApiBase/api/runs/$($Repeat.id)" -Headers $Headers
    if ($Repeated.status -notin @('queued','running')) { break }
    Start-Sleep -Milliseconds 100
  } while ((Get-Date) -lt $Deadline)
  if ($Repeated.status -ne 'completed' -or $Repeated.result.delta.embedded_chunks -ne 0 -or $Repeated.result.delta.graph_rebuilt -ne $false -or $Repeated.result.delta.reused_records -lt 1) { throw 'Unchanged packaged index was not reused.' }
  $KnowledgeUrl = "$ApiBase/api/workspaces/$($Workspace.id)/knowledge"
  $ContextUrl = "$ApiBase/api/workspaces/$($Workspace.id)/context"
  $Memory = Invoke-RestMethod $KnowledgeUrl -Headers $Headers -Method Post -ContentType 'application/json' -Body '{"content":"greet uses workspace fixtures","category":"decision","source":{"kind":"note"}}'
  $Context = Invoke-RestMethod $ContextUrl -Headers $Headers -Method Post -ContentType 'application/json' -Body '{"query":"greet workspace fixture"}'
  if (@($Context.citations | Where-Object kind -eq 'memory').Count -ne 0 -or @($Context.citations | Where-Object kind -eq 'code').Count -lt 1 -or $Context.mode -ne 'lexical') { throw 'Candidate isolation or code evidence failed.' }
  $Memory.status = 'confirmed'
  $Memory = Invoke-RestMethod $KnowledgeUrl -Headers $Headers -Method Post -ContentType 'application/json' -Body ($Memory | ConvertTo-Json -Depth 5)
  $Context = Invoke-RestMethod $ContextUrl -Headers $Headers -Method Post -ContentType 'application/json' -Body '{"query":"greet workspace fixture"}'
  if (@($Context.citations | Where-Object memory_id -eq $Memory.id).Count -ne 1) { throw 'Confirmed memory missing from context.' }
  $Replacement = Invoke-RestMethod $KnowledgeUrl -Headers $Headers -Method Post -ContentType 'application/json' -Body (@{content='greet uses durable workspace fixtures';category='decision';status='confirmed';source=@{kind='note'};supersedes=$Memory.id;supersedes_revision=$Memory.revision} | ConvertTo-Json -Depth 4)
  $RejectedStale = $false
  try { Invoke-RestMethod $KnowledgeUrl -Headers $Headers -Method Post -ContentType 'application/json' -Body ($Memory | ConvertTo-Json -Depth 5) | Out-Null }
  catch { if ([int]$_.Exception.Response.StatusCode -eq 409) { $RejectedStale = $true } else { throw } }
  if (-not $RejectedStale) { throw 'Stale memory revision was accepted.' }
  Stop-FixtureBackend
  Start-FixtureBackend
  $Recovered = Invoke-RestMethod "$ApiBase/api/runs/$($Run.id)" -Headers $Headers
  if ($Recovered.status -ne 'completed' -or $Recovered.result.project_id -ne $Result.result.project_id) { throw 'Packaged restart lost durable result.' }
  $Context = Invoke-RestMethod $ContextUrl -Headers $Headers -Method Post -ContentType 'application/json' -Body '{"query":"greet workspace fixture"}'
  if (@($Context.citations | Where-Object memory_id -eq $Replacement.id).Count -ne 1 -or @($Context.citations | Where-Object memory_id -eq $Memory.id).Count -ne 0) { throw 'Restart lost knowledge supersession.' }
  [IO.File]::WriteAllText((Join-Path $SourceRoot 'example.py'), "def changed():`n    return 'different'`n", [Text.UTF8Encoding]::new($false))
  $Context = Invoke-RestMethod $ContextUrl -Headers $Headers -Method Post -ContentType 'application/json' -Body '{"query":"greet workspace fixture"}'
  if (@($Context.citations | Where-Object kind -eq 'code').Count -ne 0 -or $Context.warnings.Count -eq 0) { throw 'Changed code source remained active after restart.' }
  $Report = @{ passed = 10; checks = @('packaged startup and workspace CRUD','real Python indexing and durable project binding','task progress and lightweight list','completed result survives backend restart','symbol index source version','unchanged index skips embeddings and graph rebuild','candidate isolation and versioned code evidence','confirmed memory context','supersession and stale revision conflict survive restart','changed source excluded with warning'); artifact = $Executable; fixture = $FixtureRoot }
  $Report | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $FixtureRoot 'result.json') -Encoding utf8
  $Report | ConvertTo-Json -Depth 4
} finally {
  Stop-FixtureBackend
  $env:NOOSPHERE_BACKEND_TOKEN = $PreviousToken
  $env:NOOSPHERE_CODELENS_SCRIPT = $PreviousScript
}
