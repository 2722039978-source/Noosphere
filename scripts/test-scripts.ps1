# Isolated regression tests: fake compilers write text files under TEMP only.
# No real application, model API, or project release is built or modified.
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
$FixtureBase = Join-Path ([IO.Path]::GetTempPath()) ('noosphere-script-tests-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $FixtureBase -Force | Out-Null
$OriginalPath = $env:PATH
$OriginalTarget = $env:CARGO_TARGET_DIR
$OriginalFixture = $env:NOOSPHERE_TEST_ROOT
$OriginalMode = $env:NOOSPHERE_TEST_MODE
$OriginalLocation = Get-Location
$script:Checks = 0
$script:FixtureNumber = 0

function Assert-Fixture {
  param([bool]$Condition, [string]$Message)
  if (-not $Condition) { throw "FAIL: $Message" }
  $script:Checks++
}

function Write-FixtureFile {
  param([string]$Root, [string]$Relative, [string]$Content)
  $Path = Join-Path $Root $Relative
  New-Item -ItemType Directory -Path (Split-Path -Parent $Path) -Force | Out-Null
  Write-NoosphereUtf8 $Path $Content
}

function New-BuildFixture {
  $script:FixtureNumber++
  $Root = Join-Path $FixtureBase "fixture $script:FixtureNumber with spaces"
  New-Item -ItemType Directory -Path (Join-Path $Root 'scripts') -Force | Out-Null
  foreach ($Name in @('build.ps1', 'dev.ps1', 'common.ps1')) {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot $Name) -Destination (Join-Path $Root "scripts/$Name")
  }
  Write-FixtureFile $Root 'VERSION' '0.5.0'
  Write-FixtureFile $Root 'backend/configs/config.yaml' 'safe-default-configuration'
  Write-FixtureFile $Root 'backend/models.yaml' 'PRIVATE-SOURCE-DO-NOT-DISTRIBUTE'
  Write-FixtureFile $Root 'backend/python/codelens/main.py' '# application source'
  Write-FixtureFile $Root 'backend/python/codelens/requirements.txt' 'fastapi'
  Write-FixtureFile $Root 'backend/python/codelens/__pycache__/cached.py' '# exclude cache'
  Write-FixtureFile $Root 'setup.bat' '@echo setup'
  Write-FixtureFile $Root 'frontend/dist/index.html' '<html>old</html>'
  Write-FixtureFile $Root 'desktop/src-tauri/target/release/noosphere-desktop.exe' 'STALE-DEFAULT-TARGET'
  Write-FixtureFile $Root 'release/Noosphere.exe' 'OLD-DESKTOP'
  Write-FixtureFile $Root 'release/bin/noosphere-backend.exe' 'OLD-BACKEND'
  Write-FixtureFile $Root 'release/config.yaml' 'USER-CONFIG'
  Write-FixtureFile $Root 'release/models.yaml' 'USER-MODELS'
  Write-FixtureFile $Root 'release/data/session.txt' 'USER-DATA'
  Write-FixtureFile $Root 'release/logs/app.log' 'USER-LOGS'
  Write-FixtureFile $Root 'release/Noosphere-old-setup.exe' 'OLD-INSTALLER'
  $env:NOOSPHERE_TEST_ROOT = $Root
  $env:NOOSPHERE_TEST_MODE = 'normal'
  $env:CARGO_TARGET_DIR = 'custom target'
  return $Root
}

function Get-FixtureContents {
  param([string]$Root, [string]$Relative)
  return [IO.File]::ReadAllText((Join-Path $Root $Relative))
}

function Assert-BuildFails {
  param([string]$Root, [hashtable]$Flags = @{}, [string]$Pattern)
  $Failed = $false
  try { & (Join-Path $Root 'scripts/build.ps1') @Flags | Out-Null } catch {
    $Failed = $true
    Assert-Fixture ($_.Exception.Message -match $Pattern) "failure should mention $Pattern, got $($_.Exception.Message)"
  }
  Assert-Fixture $Failed 'build should fail'
  Assert-Fixture ((Get-FixtureContents $Root 'release/Noosphere.exe') -eq 'OLD-DESKTOP') 'failed build must preserve previous desktop'
  Assert-Fixture ((Get-FixtureContents $Root 'release/Noosphere-old-setup.exe') -eq 'OLD-INSTALLER') 'failed build must preserve previous installer'
}

try {
  $ToolDirectory = Join-Path $FixtureBase 'fake tools'
  New-Item -ItemType Directory -Path $ToolDirectory -Force | Out-Null
  Write-NoosphereUtf8 (Join-Path $ToolDirectory 'go.ps1') @'
$OutputIndex = [Array]::IndexOf($args, '-o') + 1
if ($OutputIndex -lt 1) { throw 'missing Go output argument' }
[IO.File]::WriteAllText($args[$OutputIndex], 'NEW-BACKEND')
$global:LASTEXITCODE = 0
'@
  Write-NoosphereUtf8 (Join-Path $ToolDirectory 'npm.ps1') @'
if ((Get-Location).Path -ne (Join-Path $env:NOOSPHERE_TEST_ROOT 'frontend')) { throw 'npm ran from the wrong cwd' }
[IO.File]::WriteAllText((Join-Path (Get-Location) 'dist/index.html'), '<html>new</html>')
$global:LASTEXITCODE = 0
'@
  Write-NoosphereUtf8 (Join-Path $ToolDirectory 'cargo.ps1') @'
$TauriRoot = Join-Path $env:NOOSPHERE_TEST_ROOT 'desktop/src-tauri'
if ((Get-Location).Path -ne $TauriRoot) { throw 'cargo ran from the wrong cwd' }
if ($args[0] -eq '--list') { '    tauri        Build Tauri applications'; $global:LASTEXITCODE = 0; return }
$TargetRoot = $env:CARGO_TARGET_DIR
if (-not [IO.Path]::IsPathRooted($TargetRoot)) { $TargetRoot = Join-Path $TauriRoot $TargetRoot }
$ProfileRoot = Join-Path $TargetRoot 'release'
New-Item -ItemType Directory -Path $ProfileRoot -Force | Out-Null
if ($args[0] -eq 'tauri') {
  if ($env:NOOSPHERE_TEST_MODE -eq 'nsis-unchanged') { $global:LASTEXITCODE = 0; return }
  $InstallerRoot = Join-Path $ProfileRoot 'bundle/nsis'
  New-Item -ItemType Directory -Path $InstallerRoot -Force | Out-Null
  [IO.File]::WriteAllText((Join-Path $InstallerRoot 'Noosphere-new-setup.exe'), 'NEW-INSTALLER')
  $global:LASTEXITCODE = 0
  return
}
if ($env:NOOSPHERE_TEST_MODE -eq 'cargo-fail') { $global:LASTEXITCODE = 7; return }
if ($env:NOOSPHERE_TEST_MODE -eq 'no-artifact') { $global:LASTEXITCODE = 0; return }
$Artifact = Join-Path $ProfileRoot 'noosphere-desktop.exe'
[IO.File]::WriteAllText($Artifact, 'NEW-DESKTOP')
@{ reason = 'compiler-artifact'; target = @{ name = 'noosphere-desktop'; kind = @('bin') }; executable = $Artifact; fresh = $false } | ConvertTo-Json -Compress
$global:LASTEXITCODE = 0
'@
  $env:PATH = $ToolDirectory + [IO.Path]::PathSeparator + $OriginalPath
  Set-Location -LiteralPath $FixtureBase

  $Root = New-BuildFixture
  & (Join-Path $Root 'scripts/build.ps1') | Out-Null
  Assert-Fixture ((Get-FixtureContents $Root 'release/Noosphere.exe') -eq 'NEW-DESKTOP') 'must publish Cargo-selected custom-target binary'
  Assert-Fixture ((Get-FixtureContents $Root 'release/bin/noosphere-backend.exe') -eq 'NEW-BACKEND') 'must publish new backend'
  $BuildInfo = Get-FixtureContents $Root 'release/build-info.json' | ConvertFrom-Json
  Assert-Fixture ($BuildInfo.runtime_revision -eq 1 -and $BuildInfo.desktop_sha256 -eq (Get-FileHash -LiteralPath (Join-Path $Root 'release/Noosphere.exe')).Hash) 'manifest must identify the actual published executable'
  foreach ($Pair in @(@('config.yaml', 'USER-CONFIG'), @('models.yaml', 'USER-MODELS'), @('data/session.txt', 'USER-DATA'), @('logs/app.log', 'USER-LOGS'))) {
    Assert-Fixture ((Get-FixtureContents $Root ("release/" + $Pair[0])) -eq $Pair[1]) "must preserve $($Pair[0])"
  }
  Assert-Fixture (-not (Test-Path -LiteralPath (Join-Path $Root 'release/Noosphere-old-setup.exe'))) 'old installer must leave the main release directory'
  $Legacy = @(Get-ChildItem -LiteralPath (Join-Path $Root 'release/legacy/installers') -Recurse -File)
  Assert-Fixture ($Legacy.Count -eq 1 -and [IO.File]::ReadAllText($Legacy[0].FullName) -eq 'OLD-INSTALLER') 'old installer must be archived intact'
  $DesktopBackup = @(Get-ChildItem -LiteralPath (Join-Path $Root 'release/legacy/desktop') -Recurse -Filter 'Noosphere.exe' -File)
  Assert-Fixture ($DesktopBackup.Count -eq 1 -and [IO.File]::ReadAllText($DesktopBackup[0].FullName) -eq 'OLD-DESKTOP') 'previous executable must be archived intact'
  Assert-Fixture ((Get-FixtureContents $Root 'release/config.defaults.yaml') -eq 'safe-default-configuration') 'must ship defaults template'
  Assert-Fixture (-not (Test-Path -LiteralPath (Join-Path $Root 'release/codelens-python/__pycache__'))) 'must exclude CodeLens caches'
  Assert-Fixture ((Get-Location).Path -eq $FixtureBase) 'build must restore the caller cwd'

  $Root = New-BuildFixture
  $Before = @{}
  foreach ($File in Get-ChildItem -LiteralPath (Join-Path $Root 'release') -Recurse -File) { $Before[$File.FullName] = (Get-FileHash -LiteralPath $File.FullName).Hash }
  & (Join-Path $Root 'scripts/build.ps1') -SkipTauri | Out-Null
  foreach ($Path in $Before.Keys) { Assert-Fixture ((Get-FileHash -LiteralPath $Path).Hash -eq $Before[$Path]) 'SkipTauri must preserve every existing release file' }
  Assert-Fixture (@(Get-ChildItem -LiteralPath (Join-Path $Root 'release') -Recurse -File).Count -eq $Before.Count) 'SkipTauri must not add release files'

  $Root = New-BuildFixture
  Remove-Item -LiteralPath (Join-Path $Root 'release/config.yaml'), (Join-Path $Root 'release/models.yaml')
  & (Join-Path $Root 'scripts/build.ps1') | Out-Null
  Assert-Fixture ((Get-FixtureContents $Root 'release/config.yaml') -eq 'safe-default-configuration') 'fresh release must initialize config from defaults'
  Assert-Fixture ((Get-FixtureContents $Root 'release/models.yaml') -notmatch 'PRIVATE') 'fresh release must never copy private source model configuration'
  Assert-Fixture ((Get-FixtureContents $Root 'desktop/src-tauri/models.yaml') -notmatch 'PRIVATE') 'staging must never copy private source model configuration'

  foreach ($Mode in @('cargo-fail', 'no-artifact', 'nsis-unchanged')) {
    $Root = New-BuildFixture
    $env:NOOSPHERE_TEST_MODE = $Mode
    if ($Mode -eq 'nsis-unchanged') {
      Write-FixtureFile $Root 'desktop/src-tauri/custom target/release/bundle/nsis/Old-setup.exe' 'STALE-NSIS'
      Assert-BuildFails $Root @{BundleNsis = $true} 'no new NSIS'
    } elseif ($Mode -eq 'cargo-fail') { Assert-BuildFails $Root @{} 'exit 7' }
    else { Assert-BuildFails $Root @{} 'exactly one desktop executable' }
  }

  $Root = New-BuildFixture
  & (Join-Path $Root 'scripts/build.ps1') -BundleNsis | Out-Null
  Assert-Fixture ((Get-FixtureContents $Root 'release/Noosphere-new-setup.exe') -eq 'NEW-INSTALLER') 'must publish only a newly generated NSIS installer'
  Write-FixtureFile $Root 'desktop/node_modules/.bin/tauri.cmd' '@echo local-cli'
  $Cli = Resolve-NoosphereTauriCli $Root (Join-Path $ToolDirectory 'cargo.ps1')
  Assert-Fixture ($Cli.Command -eq (Join-Path $Root 'desktop/node_modules/.bin/tauri.cmd')) 'project-local npm Tauri CLI must take precedence'

  $Root = New-BuildFixture
  Write-FixtureFile $Root 'frontend/node_modules/vite/bin/vite.js' '// fixture only'
  $DevFailed = $false
  try { & (Join-Path $Root 'scripts/dev.ps1') -SkipBuild } catch {
    $DevFailed = $true
    Assert-Fixture ($_.Exception.Message -match 'SkipBuild requires') 'dev preflight must explain missing backend'
  }
  Assert-Fixture $DevFailed 'dev SkipBuild must not launch with a missing sidecar'
  Write-Host "PASS: $script:Checks script fixture checks" -ForegroundColor Green
} finally {
  Set-Location -LiteralPath $OriginalLocation.Path
  $env:PATH = $OriginalPath
  $env:CARGO_TARGET_DIR = $OriginalTarget
  $env:NOOSPHERE_TEST_ROOT = $OriginalFixture
  $env:NOOSPHERE_TEST_MODE = $OriginalMode
  # Verify this exact generated directory before recursive fixture cleanup.
  $ResolvedFixture = [IO.Path]::GetFullPath($FixtureBase)
  $ResolvedTemp = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
  if (-not $ResolvedFixture.StartsWith($ResolvedTemp, [StringComparison]::OrdinalIgnoreCase) -or (Split-Path -Leaf $ResolvedFixture) -notmatch '^noosphere-script-tests-[0-9a-f]{32}$') {
    throw "Refusing to clean an unexpected fixture path: $ResolvedFixture"
  }
  Remove-Item -LiteralPath $ResolvedFixture -Recurse -Force
}
