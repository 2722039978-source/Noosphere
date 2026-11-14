# Build the portable desktop release from any working directory.
# NSIS is optional and needs the Tauri CLI (npm @tauri-apps/cli or cargo-tauri).
param(
  [switch]$SkipTauri,
  [switch]$SkipFrontend,
  [switch]$BundleNsis,
  [string]$ReleaseRoot,
  [switch]$DesktopShortcut,
  [switch]$PackageStage
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
$ProjectRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$TauriRoot = Join-Path $ProjectRoot 'desktop/src-tauri'
$FrontendRoot = Join-Path $ProjectRoot 'frontend'
$Version = Get-NoosphereVersion $ProjectRoot
Assert-NoosphereVersionFiles $ProjectRoot $Version
$Target = 'x86_64-pc-windows-msvc'

if ($SkipTauri -and $BundleNsis) { throw '-SkipTauri and -BundleNsis cannot be combined.' }
if ($SkipTauri -and $DesktopShortcut) { throw '-DesktopShortcut requires a complete desktop build; remove -SkipTauri.' }
if (-not $ReleaseRoot) { $ReleaseRoot = Join-Path $ProjectRoot 'release' }
$ReleaseRoot = [IO.Path]::GetFullPath($ReleaseRoot)
$GoCommand = Resolve-NoosphereCommand 'go'
if (-not $SkipFrontend) { $NpmCommand = Resolve-NoosphereCommand 'npm' }
if (-not $SkipTauri) {
  $CargoCommand = Resolve-NoosphereCommand 'cargo'
  $PythonCommand = Resolve-NoosphereCommand 'python'
  if ($BundleNsis) { $TauriCli = Resolve-NoosphereTauriCli $ProjectRoot $CargoCommand }
}

Write-Host "Building Noosphere $Version" -ForegroundColor Cyan
$ReaRuntime = if (-not $SkipTauri) { Initialize-NoosphereRea $ProjectRoot }
$CodeLensRuntime = if (-not $SkipTauri) { Initialize-NoosphereCodeLensRuntime $ProjectRoot $PythonCommand }
$CrtRuntime = if (-not $SkipTauri) { Find-NoosphereCrt }
$BackendArtifact = Build-NoosphereBackend $ProjectRoot $GoCommand $Version
Initialize-NoosphereResources $ProjectRoot -ReaRuntime $ReaRuntime -CodeLensRuntime $CodeLensRuntime -CrtRuntime $CrtRuntime
if ($CodeLensRuntime) {
  Invoke-NoosphereCommand (Join-Path $TauriRoot 'codelens-runtime/python.exe') @('-B', (Join-Path $PSScriptRoot 'codelens-runtime/verify.py')) $TauriRoot
}

if (-not $SkipFrontend) {
  Invoke-NoosphereCommand $NpmCommand @('run', 'build') $FrontendRoot
} elseif (-not (Test-Path -LiteralPath (Join-Path $FrontendRoot 'dist/index.html'))) {
  throw '-SkipFrontend requires an existing frontend/dist/index.html. Run a normal build first.'
}

if ($SkipTauri) {
  Write-Host 'Backend and frontend development artifacts updated. release/ was not changed; run a full build to update the desktop app.' -ForegroundColor Yellow
  return
}

# Cargo resolves CARGO_TARGET_DIR and .cargo/config.toml target settings itself.
$CargoOutput = @(Invoke-NoosphereCommand $CargoCommand @('build', '--release', '--locked', '--target', $Target, '--bin', 'noosphere-desktop', '--message-format=json-render-diagnostics') $TauriRoot)
$DesktopArtifact = Get-NoosphereDesktopArtifact $CargoOutput
$InstallerArtifacts = @()

if ($BundleNsis) {
  $InstallerDirectory = Join-Path (Split-Path -Parent $DesktopArtifact) 'bundle/nsis'
  $InstallerBefore = Get-NoosphereFileSnapshot $InstallerDirectory
  $TauriArguments = @($TauriCli.Prefix) + @('bundle', '--bundles', 'nsis', '--target', $Target, '--ci')
  Invoke-NoosphereCommand $TauriCli.Command $TauriArguments $TauriRoot
  $InstallerArtifacts = @(Get-NoosphereChangedInstallers $InstallerDirectory $InstallerBefore)
  if ($InstallerArtifacts.Count -ne 1) {
    throw 'Tauri must produce exactly one fresh NSIS installer. Existing installers will not be presented as a new build.'
  }
}

$Toolchains = @{
  cargo = (Invoke-NoosphereCommand $CargoCommand @('--version') $TauriRoot | Out-String).Trim()
  go = (Invoke-NoosphereCommand $GoCommand @('version') (Join-Path $ProjectRoot 'backend') | Out-String).Trim()
  node = (Invoke-NoosphereCommand (Resolve-NoosphereCommand 'node') @('--version') $FrontendRoot | Out-String).Trim()
  powershell = $PSVersionTable.PSVersion.ToString()
  crt = (Get-Item -LiteralPath (Join-Path $CrtRuntime 'vcruntime140.dll')).VersionInfo.FileVersion
  build_target = $Target
  frontend_reused = [bool]$SkipFrontend
}
if ($BundleNsis) {
  $Toolchains.tauri_cli = (Invoke-NoosphereCommand $TauriCli.Command (@($TauriCli.Prefix) + @('--version')) $TauriRoot | Out-String).Trim()
}
Publish-NoosphereRelease $ProjectRoot $DesktopArtifact $BackendArtifact $InstallerArtifacts -ReleaseRoot $ReleaseRoot `
  -ReaRuntime $ReaRuntime -CodeLensRuntime $CodeLensRuntime -CrtRuntime $CrtRuntime -Toolchains $Toolchains -PackageStage:$PackageStage
if ($DesktopShortcut) { & (Join-Path $PSScriptRoot 'create-shortcuts.ps1') -Desktop -ReleaseRoot $ReleaseRoot }
Write-Host "Portable release ready: $(Join-Path $ReleaseRoot 'Noosphere.exe')" -ForegroundColor Green
if ($InstallerArtifacts.Count -eq 0) {
  Write-Host 'NSIS was not requested. Older installers, if any, are retained under release/legacy/installers.'
}
