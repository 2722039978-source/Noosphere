# Build the portable desktop release from any working directory.
# NSIS is optional and needs the Tauri CLI (npm @tauri-apps/cli or cargo-tauri).
param(
  [switch]$SkipTauri,
  [switch]$SkipFrontend,
  [switch]$BundleNsis,
  [string]$ReleaseRoot,
  [switch]$DesktopShortcut
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
$ProjectRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$TauriRoot = Join-Path $ProjectRoot 'desktop/src-tauri'
$FrontendRoot = Join-Path $ProjectRoot 'frontend'
$Version = Get-NoosphereVersion $ProjectRoot

if ($SkipTauri -and $BundleNsis) { throw '-SkipTauri and -BundleNsis cannot be combined.' }
if ($SkipTauri -and $DesktopShortcut) { throw '-DesktopShortcut requires a complete desktop build; remove -SkipTauri.' }
if (-not $ReleaseRoot) { $ReleaseRoot = Join-Path $ProjectRoot 'release' }
$ReleaseRoot = [IO.Path]::GetFullPath($ReleaseRoot)
$GoCommand = Resolve-NoosphereCommand 'go'
if (-not $SkipFrontend) { $NpmCommand = Resolve-NoosphereCommand 'npm' }
if (-not $SkipTauri) {
  $CargoCommand = Resolve-NoosphereCommand 'cargo'
  if ($BundleNsis) { $TauriCli = Resolve-NoosphereTauriCli $ProjectRoot $CargoCommand }
}

Write-Host "Building Noosphere $Version" -ForegroundColor Cyan
$BackendArtifact = Build-NoosphereBackend $ProjectRoot $GoCommand $Version
Initialize-NoosphereResources $ProjectRoot

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
$CargoOutput = @(Invoke-NoosphereCommand $CargoCommand @('build', '--release', '--locked', '--bin', 'noosphere-desktop', '--message-format=json-render-diagnostics') $TauriRoot)
$DesktopArtifact = Get-NoosphereDesktopArtifact $CargoOutput
$InstallerArtifacts = @()

if ($BundleNsis) {
  $InstallerDirectory = Join-Path (Split-Path -Parent $DesktopArtifact) 'bundle/nsis'
  $InstallerBefore = Get-NoosphereFileSnapshot $InstallerDirectory
  $OverrideFile = Join-Path ([IO.Path]::GetTempPath()) ('noosphere-bundle-' + [guid]::NewGuid().ToString('N') + '.json')
  try {
    # The frontend was built above; do not rebuild it from another working directory.
    Write-NoosphereUtf8 $OverrideFile '{"build":{"beforeBuildCommand":""}}'
    $TauriArguments = @($TauriCli.Prefix) + @('build', '--bundles', 'nsis', '--ci', '--config', $OverrideFile, '--', '--locked')
    Invoke-NoosphereCommand $TauriCli.Command $TauriArguments $TauriRoot
    $InstallerArtifacts = @(Get-NoosphereChangedInstallers $InstallerDirectory $InstallerBefore)
    if ($InstallerArtifacts.Count -eq 0) {
      throw 'Tauri reported success but produced no new NSIS installer. Existing installers will not be presented as a new build.'
    }
  } finally {
    if (Test-Path -LiteralPath $OverrideFile) { Remove-Item -LiteralPath $OverrideFile -Force }
  }
}

# Publication starts only after every requested build has succeeded.
Publish-NoosphereRelease $ProjectRoot $DesktopArtifact $BackendArtifact $InstallerArtifacts -ReleaseRoot $ReleaseRoot
if ($DesktopShortcut) { & (Join-Path $PSScriptRoot 'create-shortcuts.ps1') -Desktop -ReleaseRoot $ReleaseRoot }
Write-Host "Portable release ready: $(Join-Path $ReleaseRoot 'Noosphere.exe')" -ForegroundColor Green
if ($InstallerArtifacts.Count -eq 0) {
  Write-Host 'NSIS was not requested. Older installers, if any, are retained under release/legacy/installers.'
}
