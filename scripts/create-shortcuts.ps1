# Recreate portable launch shortcuts after moving the project folder.
param([switch]$Desktop, [string]$ReleaseRoot, [string]$ExpectedSourceRevision, [string]$ExpectedSourceFingerprint)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
$ProjectRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
if (-not $ReleaseRoot) { $ReleaseRoot = Join-Path $ProjectRoot 'release' }
$ReleaseRoot = [IO.Path]::GetFullPath($ReleaseRoot)
$Source = Get-NoosphereSourceIdentity $ProjectRoot
if (-not $ExpectedSourceRevision) { $ExpectedSourceRevision = $Source.revision }
if (-not $ExpectedSourceFingerprint) { $ExpectedSourceFingerprint = $Source.fingerprint }
$null = Get-NoosphereReleaseInfo $ReleaseRoot $ExpectedSourceRevision $ExpectedSourceFingerprint
$ShortcutFolders = @($ProjectRoot)
if ($Desktop) { $ShortcutFolders += [Environment]::GetFolderPath('DesktopDirectory') }
foreach ($Folder in $ShortcutFolders | Select-Object -Unique) {
  $LinkPath = Join-Path $Folder 'Noosphere.lnk'
  New-NoosphereShortcut $LinkPath $ReleaseRoot $ExpectedSourceRevision $ExpectedSourceFingerprint
}
