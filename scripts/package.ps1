# Build the official Windows x64 installer and portable ZIP in a fresh directory.
param([string]$OutputRoot)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
$ProjectRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$Version = Get-NoosphereVersion $ProjectRoot
if (-not $OutputRoot) { $OutputRoot = Join-Path $ProjectRoot 'artifacts' }
$OutputRoot = [IO.Path]::GetFullPath($OutputRoot)
$Destination = Join-Path $OutputRoot $Version
if (Test-Path -LiteralPath $Destination) {
  throw "Package destination already exists: $Destination. Use a new -OutputRoot or move the previous output aside."
}

$Workspace = Join-Path $ProjectRoot ('test/package-' + [guid]::NewGuid().ToString('N'))
$Portable = Join-Path $Workspace 'Noosphere'
$Name = "Noosphere-$Version-windows-x64"
New-Item -ItemType Directory -Path $Workspace, $OutputRoot -Force | Out-Null
# Keep final publication on one volume; a failed build never appears at the versioned output.
$Stage = Join-Path $OutputRoot ('.stage-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Stage | Out-Null

& (Join-Path $PSScriptRoot 'build.ps1') -BundleNsis -PackageStage -ReleaseRoot $Portable
$Info = Get-NoosphereReleaseInfo $Portable
if (-not $Info.codelens_runtime -or -not $Info.rea_runtime -or $Info.installers.Count -ne 1) {
  throw 'Package build must include CodeLens, REA and exactly one NSIS installer.'
}
foreach ($Forbidden in @('config.yaml', 'models.yaml', 'data', 'logs', 'legacy', '.venv', 'setup.bat')) {
  if (Test-Path -LiteralPath (Join-Path $Portable $Forbidden)) { throw "Unexpected private or development resource: $Forbidden" }
}
Copy-Item -LiteralPath (Join-Path $ProjectRoot 'LICENSE') -Destination $Portable
Copy-Item -LiteralPath (Join-Path $ProjectRoot 'frontend/public/assets/RhineLabUI-LICENSE.txt') -Destination $Portable
New-Item -ItemType Directory -Path (Join-Path $Portable 'scripts') | Out-Null
$OpenVino = Join-Path $Portable 'scripts/openvino'
New-Item -ItemType Directory -Path $OpenVino | Out-Null
foreach ($File in @('Install-OpenVINO.ps1', 'Start-OpenVINO.ps1', 'Stop-OpenVINO.ps1', 'Test-OpenVINO.ps1',
    'USAGE.txt', 'bootstrap.py', 'download_model.py', 'launcher.py', 'model.lock.json',
    'requirements-win-py314.lock', 'requirements.txt', 'server.py')) {
  Copy-Item -LiteralPath (Join-Path $ProjectRoot "scripts/openvino/$File") -Destination $OpenVino
}
Write-NoosphereUtf8 (Join-Path $Portable 'START-HERE.txt') @"
Noosphere $Version · Windows x64

Extract this complete archive to a folder, then launch Noosphere.exe.
All internal application services are included. The portable application
requires Microsoft Edge WebView2 Runtime; use the Noosphere installer if it
is missing. Application data is stored under the current Windows profile
unless an existing portable configuration or a custom data root selects it.
See the project README for model providers, user data and optional OpenVINO.
"@

$Installer = Join-Path $Stage "$Name-setup.exe"
Move-Item -LiteralPath (Join-Path $Portable $Info.installers[0]) -Destination $Installer
$Info.installers = @("$Name-setup.exe")
Write-NoosphereUtf8 (Join-Path $Portable 'build-info.json') ($Info | ConvertTo-Json -Depth 6)
Copy-Item -LiteralPath (Join-Path $Portable 'build-info.json') -Destination $Stage
$Zip = Join-Path $Stage "$Name.zip"
[IO.Compression.ZipFile]::CreateFromDirectory($Portable, $Zip, 'Optimal', $true)

$Archive = [IO.Compression.ZipFile]::OpenRead($Zip)
try {
  $ArchiveEntries = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
  foreach ($Entry in $Archive.Entries) {
    if (-not $ArchiveEntries.Add($Entry.FullName.Replace('\', '/'))) { throw "Duplicate ZIP entry $($Entry.FullName)." }
  }
  foreach ($File in Get-ChildItem -LiteralPath $Portable -Recurse -Force -File) {
    $EntryName = 'Noosphere/' + [IO.Path]::GetRelativePath($Portable, $File.FullName).Replace('\', '/')
    if (-not $ArchiveEntries.Contains($EntryName)) { throw "ZIP missing $EntryName." }
  }
} finally { $Archive.Dispose() }

$Extracted = Join-Path $Workspace 'verify'
[IO.Compression.ZipFile]::ExtractToDirectory($Zip, $Extracted)
$ExtractedPortable = Join-Path $Extracted 'Noosphere'
$null = Get-NoosphereReleaseInfo $ExtractedPortable $Info.source_revision $Info.source_fingerprint
Invoke-NoosphereCommand (Join-Path $ExtractedPortable 'codelens-runtime/python.exe') @('-B', (Join-Path $PSScriptRoot 'codelens-runtime/verify.py')) $ExtractedPortable

$Hashes = foreach ($Path in @($Installer, $Zip, (Join-Path $Stage 'build-info.json'))) {
  $Hash = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
  "$Hash  $(Split-Path -Leaf $Path)"
}
Write-NoosphereUtf8 (Join-Path $Stage 'SHA256SUMS.txt') (($Hashes -join "`n") + "`n")
[IO.Directory]::Move($Stage, $Destination)
Write-Host "Verified package artifacts: $Destination"
