# Shared helpers. Dot-sourcing this file does not build or publish anything.

function Resolve-NoosphereCommand {
  param([string]$Name)
  $Command = Get-Command $Name -CommandType Application, ExternalScript -ErrorAction SilentlyContinue | Select-Object -First 1
  if (-not $Command) {
    $LocalRoot = Join-Path (Split-Path -Parent $PSScriptRoot) '.local'
    $Relative = @{ go = 'tools/go/bin/go.exe'; cargo = 'tools/cargo/bin/cargo.exe'; python = 'venv/Scripts/python.exe' }[$Name]
    if ($Relative -and (Test-Path -LiteralPath (Join-Path $LocalRoot $Relative))) {
      if ($Name -eq 'cargo') {
        $env:CARGO_HOME = Join-Path $LocalRoot 'tools/cargo'
        $env:RUSTUP_HOME = Join-Path $LocalRoot 'tools/rustup'
        $env:PATH = (Join-Path $env:CARGO_HOME 'bin') + ';' + $env:PATH
      }
      return Join-Path $LocalRoot $Relative
    }
  }
  if (-not $Command) { throw "Required tool '$Name' was not found on PATH. Install it and reopen the terminal, or add its bin directory to PATH for this session." }
  return $Command.Source
}

function Resolve-NoosphereTauriCli {
  param([string]$ProjectRoot, [string]$CargoCommand)
  foreach ($Candidate in @('desktop/node_modules/.bin/tauri.cmd', 'frontend/node_modules/.bin/tauri.cmd', 'node_modules/.bin/tauri.cmd')) {
    $LocalCli = Join-Path $ProjectRoot $Candidate
    if (Test-Path -LiteralPath $LocalCli) { return @{ Command = $LocalCli; Prefix = @() } }
  }
  $PathCli = Get-Command 'tauri.cmd' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($PathCli) { return @{ Command = $PathCli.Source; Prefix = @() } }
  $CargoCommands = Invoke-NoosphereCommand $CargoCommand @('--list') (Join-Path $ProjectRoot 'desktop/src-tauri')
  if (($CargoCommands -join "`n") -match '(?m)^\s+tauri\s') { return @{ Command = $CargoCommand; Prefix = @('tauri') } }
  throw 'NSIS packaging needs the Tauri CLI. Install @tauri-apps/cli locally or cargo install tauri-cli --locked; omit -BundleNsis for a complete portable release.'
}

function Invoke-NoosphereCommand {
  param([string]$Command, [string[]]$Arguments, [string]$WorkingDirectory)
  Push-Location -LiteralPath $WorkingDirectory
  try {
    $global:LASTEXITCODE = 0
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$(Split-Path -Leaf $Command) failed (exit $LASTEXITCODE)." }
  } finally { Pop-Location }
}

function Get-NoosphereVersion {
  param([string]$ProjectRoot)
  $Version = (Get-Content -LiteralPath (Join-Path $ProjectRoot 'VERSION') -Raw).Trim()
  if ($Version -notmatch '^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$') { throw 'VERSION must contain a semantic version.' }
  return $Version
}

function Write-NoosphereUtf8 {
  param([string]$Path, [string]$Content)
  [IO.File]::WriteAllText($Path, $Content, [Text.UTF8Encoding]::new($false))
}

function Build-NoosphereBackend {
  param([string]$ProjectRoot, [string]$GoCommand, [string]$Version)
  $BackendBin = Join-Path $ProjectRoot 'desktop/src-tauri/bin'
  New-Item -ItemType Directory -Path $BackendBin -Force | Out-Null
  $Artifact = Join-Path $BackendBin 'noosphere-backend.exe'
  Invoke-NoosphereCommand $GoCommand @('build', '-ldflags', "-X main.Version=$Version -s -w", '-o', $Artifact, './cmd/server') (Join-Path $ProjectRoot 'backend') | Out-Host
  if (-not (Test-Path -LiteralPath $Artifact -PathType Leaf)) { throw 'Go did not produce the requested backend executable.' }
  return $Artifact
}

function Copy-NoosphereCodeLens {
  param([string]$ProjectRoot, [string]$Destination)
  $Source = Join-Path $ProjectRoot 'backend/python/codelens'
  if (-not (Test-Path -LiteralPath (Join-Path $Source 'main.py'))) { throw 'CodeLens source main.py is missing.' }
  New-Item -ItemType Directory -Path $Destination -Force | Out-Null
  foreach ($File in Get-ChildItem -LiteralPath $Source -Recurse -File) {
    $Relative = $File.FullName.Substring($Source.Length).TrimStart([char[]]@('\', '/'))
    if ($Relative -match '(^|[\\/])(__pycache__|\.venv|venv|\.pytest_cache)([\\/]|$)') { continue }
    if ($File.Extension -ne '.py' -and $File.Name -ne 'requirements.txt') { continue }
    if ($File.Name -like 'test_*.py') { continue }
    $Target = Join-Path $Destination $Relative
    New-Item -ItemType Directory -Path (Split-Path -Parent $Target) -Force | Out-Null
    Copy-Item -LiteralPath $File.FullName -Destination $Target -Force
  }
}

function Initialize-NoosphereResources {
  param([string]$ProjectRoot)
  $TauriRoot = Join-Path $ProjectRoot 'desktop/src-tauri'
  $Defaults = Join-Path $TauriRoot 'config.defaults.yaml'
  Copy-Item -LiteralPath (Join-Path $ProjectRoot 'backend/configs/config.yaml') -Destination $Defaults -Force
  if (-not (Test-Path -LiteralPath (Join-Path $TauriRoot 'config.yaml'))) {
    Copy-Item -LiteralPath $Defaults -Destination (Join-Path $TauriRoot 'config.yaml')
  }
  if (-not (Test-Path -LiteralPath (Join-Path $TauriRoot 'models.yaml'))) {
    Write-NoosphereUtf8 (Join-Path $TauriRoot 'models.yaml') "default_model: `"`"`nproviders: []`n"
  }
  Copy-NoosphereCodeLens $ProjectRoot (Join-Path $TauriRoot 'codelens-python')
  Copy-Item -LiteralPath (Join-Path $ProjectRoot 'setup.bat') -Destination (Join-Path $TauriRoot 'setup.bat') -Force
}

function Get-NoosphereDesktopArtifact {
  param([object[]]$CargoOutput)
  $Artifacts = @()
  foreach ($Line in $CargoOutput) {
    if ([string]::IsNullOrWhiteSpace([string]$Line)) { continue }
    try { $Event = [string]$Line | ConvertFrom-Json } catch { Write-Host $Line; continue }
    if ($Event.reason -eq 'compiler-message' -and $Event.message.rendered) { Write-Host $Event.message.rendered }
    if ($Event.reason -eq 'compiler-artifact' -and $Event.target.name -eq 'noosphere-desktop' -and $Event.target.kind -contains 'bin' -and $Event.executable) {
      $Artifacts += [string]$Event.executable
    }
  }
  $Artifacts = @($Artifacts | Select-Object -Unique)
  if ($Artifacts.Count -ne 1) { throw 'Cargo did not identify exactly one desktop executable for this build. No old executable will be copied.' }
  if (-not (Test-Path -LiteralPath $Artifacts[0] -PathType Leaf)) { throw "Cargo artifact is missing: $($Artifacts[0])" }
  return [IO.Path]::GetFullPath($Artifacts[0])
}

function Get-NoosphereFileSnapshot {
  param([string]$Directory)
  $Snapshot = @{}
  if (Test-Path -LiteralPath $Directory) {
    foreach ($File in Get-ChildItem -LiteralPath $Directory -Filter '*.exe' -File) {
      $Snapshot[$File.FullName] = "$($File.LastWriteTimeUtc.Ticks):$($File.Length):$((Get-FileHash -LiteralPath $File.FullName -Algorithm SHA256).Hash)"
    }
  }
  return $Snapshot
}

function Get-NoosphereChangedInstallers {
  param([string]$Directory, [hashtable]$Before)
  $After = Get-NoosphereFileSnapshot $Directory
  foreach ($Path in $After.Keys) {
    if (-not $Before.ContainsKey($Path) -or $Before[$Path] -ne $After[$Path]) { $Path }
  }
}

function Publish-NoosphereRelease {
  param([string]$ProjectRoot, [string]$DesktopArtifact, [string]$BackendArtifact, [string[]]$InstallerArtifacts = @())
  foreach ($Artifact in (@($DesktopArtifact, $BackendArtifact) + $InstallerArtifacts)) {
    if (-not (Test-Path -LiteralPath $Artifact -PathType Leaf)) { throw "Build artifact is missing: $Artifact" }
  }
  $ReleaseRoot = Join-Path $ProjectRoot 'release'
  $ReleaseBin = Join-Path $ReleaseRoot 'bin'
  New-Item -ItemType Directory -Path $ReleaseBin -Force | Out-Null
  $PreviousDesktop = Join-Path $ReleaseRoot 'Noosphere.exe'
  if (Test-Path -LiteralPath $PreviousDesktop -PathType Leaf) {
    $BackupRoot = Join-Path $ReleaseRoot ('legacy/desktop/' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Path $BackupRoot -Force | Out-Null
    Copy-Item -LiteralPath $PreviousDesktop -Destination (Join-Path $BackupRoot 'Noosphere.exe')
    $PreviousBackend = Join-Path $ReleaseBin 'noosphere-backend.exe'
    if (Test-Path -LiteralPath $PreviousBackend) { Copy-Item -LiteralPath $PreviousBackend -Destination (Join-Path $BackupRoot 'noosphere-backend.exe') }
  }
  Copy-Item -LiteralPath $DesktopArtifact -Destination (Join-Path $ReleaseRoot 'Noosphere.exe') -Force
  Copy-Item -LiteralPath $BackendArtifact -Destination (Join-Path $ReleaseBin 'noosphere-backend.exe') -Force
  Copy-Item -LiteralPath (Join-Path $ProjectRoot 'desktop/src-tauri/config.defaults.yaml') -Destination (Join-Path $ReleaseRoot 'config.defaults.yaml') -Force
  if (-not (Test-Path -LiteralPath (Join-Path $ReleaseRoot 'config.yaml'))) {
    Copy-Item -LiteralPath (Join-Path $ReleaseRoot 'config.defaults.yaml') -Destination (Join-Path $ReleaseRoot 'config.yaml')
  }
  if (-not (Test-Path -LiteralPath (Join-Path $ReleaseRoot 'models.yaml'))) {
    Write-NoosphereUtf8 (Join-Path $ReleaseRoot 'models.yaml') "default_model: `"`"`nproviders: []`n"
  }
  Copy-NoosphereCodeLens $ProjectRoot (Join-Path $ReleaseRoot 'codelens-python')
  Copy-Item -LiteralPath (Join-Path $ProjectRoot 'setup.bat') -Destination (Join-Path $ReleaseRoot 'setup.bat') -Force

  $OldInstallers = @(Get-ChildItem -LiteralPath $ReleaseRoot -Filter '*setup*.exe' -File)
  if ($OldInstallers.Count -gt 0) {
    $LegacyRoot = Join-Path $ReleaseRoot ('legacy/installers/' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Path $LegacyRoot -Force | Out-Null
    foreach ($OldInstaller in $OldInstallers) { Move-Item -LiteralPath $OldInstaller.FullName -Destination (Join-Path $LegacyRoot $OldInstaller.Name) }
  }
  foreach ($Installer in $InstallerArtifacts) { Copy-Item -LiteralPath $Installer -Destination (Join-Path $ReleaseRoot (Split-Path -Leaf $Installer)) -Force }
  $BuildInfo = [ordered]@{
    version = Get-NoosphereVersion $ProjectRoot
    built_at_utc = [DateTime]::UtcNow.ToString('o')
    runtime_revision = 1
    desktop_sha256 = (Get-FileHash -LiteralPath (Join-Path $ReleaseRoot 'Noosphere.exe') -Algorithm SHA256).Hash
    backend_sha256 = (Get-FileHash -LiteralPath (Join-Path $ReleaseBin 'noosphere-backend.exe') -Algorithm SHA256).Hash
    installers = @($InstallerArtifacts | ForEach-Object { Split-Path -Leaf $_ })
  }
  Write-NoosphereUtf8 (Join-Path $ReleaseRoot 'build-info.json') ($BuildInfo | ConvertTo-Json -Depth 3)
}
