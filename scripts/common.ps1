# Shared helpers. Dot-sourcing this file does not build or publish anything.

. (Join-Path $PSScriptRoot 'codelens-runtime/prepare.ps1')

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

function Assert-NoosphereVersionFiles {
  param([string]$ProjectRoot, [string]$Version)
  foreach ($Path in @('frontend/package.json', 'frontend/package-lock.json', 'desktop/package.json',
      'desktop/package-lock.json', 'desktop/src-tauri/tauri.conf.json')) {
    $Document = Get-Content -LiteralPath (Join-Path $ProjectRoot $Path) -Raw | ConvertFrom-Json -AsHashtable
    if ($Document.version -ne $Version -or ($Document.packages -and $Document.packages[''].version -ne $Version)) {
      throw "$Path must match VERSION ($Version)."
    }
  }
  $Manifest = Get-Content -LiteralPath (Join-Path $ProjectRoot 'desktop/src-tauri/Cargo.toml') -Raw
  if ($Manifest -notmatch '(?m)^version = "([^"]+)"' -or $Matches[1] -ne $Version) {
    throw "desktop/src-tauri/Cargo.toml must match VERSION ($Version)."
  }
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
  $Environment = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
  try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    Invoke-NoosphereCommand $GoCommand @('build', '-trimpath', '-ldflags', "-X main.Version=$Version -s -w", '-o', $Artifact, './cmd/server') (Join-Path $ProjectRoot 'backend') | Out-Host
  } finally {
    foreach ($Name in $Environment.Keys) { [Environment]::SetEnvironmentVariable($Name, $Environment[$Name], 'Process') }
  }
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

function Initialize-NoosphereRea {
  param([string]$ProjectRoot)
  if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') { throw 'REA 分发目前支持 Windows x64。' }
  $Recipe = Join-Path $ProjectRoot 'scripts/rea-runtime'
  $Cache = if ($env:NOOSPHERE_REA_BUILD_CACHE) { $env:NOOSPHERE_REA_BUILD_CACHE } else { Join-Path $ProjectRoot 'test/cache/rea-runtime' }
  if (([IO.Path]::GetPathRoot($Cache)).Length -lt 3) { throw 'REA 构建缓存必须使用绝对路径。' }
  $NodeName = 'node-v24.22.2-win-x64'
  $NodeZip = Join-Path $Cache "$NodeName.zip"
  $NodeSHA = 'CA1BFF5D8EAD13C9F2EA404A176DB46BD430704D5B5113734049FA365E7799E2'
  $LockFile = Join-Path $Recipe 'package-lock.json'
  $LockHash = (Get-FileHash -LiteralPath $LockFile -Algorithm SHA256).Hash
  $Runtime = Join-Path $Cache $LockHash
  $NodeRoot = Join-Path $Cache $NodeName
  $ReaScript = 'node_modules/rea-agents/scripts/rea.mjs'
  New-Item -ItemType Directory -Path $Cache, $Runtime -Force | Out-Null
  $Lock = [IO.File]::Open((Join-Path $Cache 'prepare.lock'), [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
  try {
    if (-not (Test-Path -LiteralPath $NodeZip -PathType Leaf)) {
      $Download = $NodeZip + '.download'
      Invoke-WebRequest -UseBasicParsing -Uri "https://nodejs.org/dist/v24.22.2/$NodeName.zip" -OutFile $Download
      if ((Get-FileHash -LiteralPath $Download -Algorithm SHA256).Hash -ne $NodeSHA) { throw 'Node.js 下载文件的 SHA-256 校验失败。' }
      Move-Item -LiteralPath $Download -Destination $NodeZip
    }
    if ((Get-FileHash -LiteralPath $NodeZip -Algorithm SHA256).Hash -ne $NodeSHA) { throw 'Node.js 缓存文件的 SHA-256 校验失败。' }
    $NpmScript = Join-Path $NodeRoot 'node_modules/npm/bin/npm-cli.js'
    if (-not (Test-Path -LiteralPath $NpmScript -PathType Leaf)) { Expand-Archive -LiteralPath $NodeZip -DestinationPath $Cache -Force }
    $Node = Join-Path $NodeRoot 'node.exe'
    $Stamp = Join-Path $Runtime 'distribution.json'
    if (-not (Test-Path -LiteralPath $Stamp -PathType Leaf)) {
      Copy-Item -LiteralPath (Join-Path $Recipe 'package.json'), $LockFile -Destination $Runtime -Force
      Invoke-NoosphereCommand $Node @($NpmScript, 'ci', '--ignore-scripts', '--omit=optional', '--no-audit', '--no-fund', '--cache', (Join-Path $Cache 'npm')) $Runtime | Out-Host
      Copy-Item -LiteralPath $Node -Destination (Join-Path $Runtime 'node.exe') -Force
      Copy-Item -LiteralPath (Join-Path $NodeRoot 'LICENSE') -Destination (Join-Path $Runtime 'NODE-LICENSE.txt') -Force
    }
    $BundledNode = Join-Path $Runtime 'node.exe'
    $ActualNode = (Invoke-NoosphereCommand $BundledNode @('--version') $Runtime | Out-String).Trim()
    $ActualRea = (Invoke-NoosphereCommand $BundledNode @((Join-Path $Runtime $ReaScript), '--version') $Runtime | Out-String).Trim()
    if ($ActualNode -ne 'v24.22.2' -or $ActualRea -ne '6.8.0') { throw "REA 运行组件版本校验失败。Node.js $ActualNode，REA $ActualRea。" }
    Write-NoosphereUtf8 $Stamp (@{ node = $ActualNode; rea = $ActualRea; lock_sha256 = $LockHash } | ConvertTo-Json -Compress)
    return $Runtime
  } finally { $Lock.Dispose() }
}

function Copy-NoosphereRea {
  param([string]$Source, [string]$DestinationRoot)
  $Source = [IO.Path]::GetFullPath($Source)
  $DestinationRoot = [IO.Path]::GetFullPath($DestinationRoot)
  $Destination = [IO.Path]::GetFullPath((Join-Path $DestinationRoot 'rea-runtime'))
  $Boundary = $DestinationRoot.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
  if ($Source -eq $Destination -or -not $Destination.StartsWith($Boundary, [StringComparison]::OrdinalIgnoreCase)) { throw 'REA 分发目录无效。' }
  $Stamp = Join-Path $Source 'distribution.json'
  $Existing = Join-Path $Destination 'distribution.json'
  if ((Test-Path -LiteralPath $Existing -PathType Leaf) -and (Test-Path -LiteralPath (Join-Path $Destination 'node_modules/rea-agents/scripts/rea.mjs') -PathType Leaf) -and (Test-Path -LiteralPath (Join-Path $Destination 'node.exe') -PathType Leaf) -and (Get-Content -Raw -LiteralPath $Existing) -eq (Get-Content -Raw -LiteralPath $Stamp)) { return }
  if (Test-Path -LiteralPath $Destination) {
    $Item = Get-Item -LiteralPath $Destination -Force
    if (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'REA 分发目录不能是链接。' }
    Remove-Item -LiteralPath $Destination -Recurse -Force
  }
  New-Item -ItemType Directory -Path $Destination -Force | Out-Null
  foreach ($Name in @('node.exe', 'NODE-LICENSE.txt', 'node_modules', 'package.json', 'package-lock.json', 'distribution.json')) {
    Copy-Item -LiteralPath (Join-Path $Source $Name) -Destination $Destination -Recurse
  }
}

function Initialize-NoosphereResources {
  param([string]$ProjectRoot, [string]$ReaRuntime, [string]$CodeLensRuntime, [string]$CrtRuntime)
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
  if ($ReaRuntime) { Copy-NoosphereRea $ReaRuntime $TauriRoot }
  New-Item -ItemType Directory -Path (Join-Path $TauriRoot 'codelens-runtime'), (Join-Path $TauriRoot 'redist') -Force | Out-Null
  if ($CodeLensRuntime) { Copy-NoosphereCodeLensRuntime $CodeLensRuntime $TauriRoot }
  if ($CrtRuntime) { Copy-NoosphereCrt $CrtRuntime (Join-Path $TauriRoot 'redist') }
  Copy-Item -LiteralPath (Join-Path $ProjectRoot 'setup.bat') -Destination (Join-Path $TauriRoot 'setup.bat') -Force
}

function Find-NoosphereCrt {
  $ProgramFilesX86 = ${env:ProgramFiles(x86)}
  if (-not $ProgramFilesX86) { throw 'Visual C++ runtime staging requires Windows x64.' }
  $VsWhere = Join-Path $ProgramFilesX86 'Microsoft Visual Studio/Installer/vswhere.exe'
  $Redist = @($env:VCToolsRedistDir | Where-Object { $_ })
  if (Test-Path -LiteralPath $VsWhere -PathType Leaf) {
    $Roots = @(& $VsWhere -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath)
    foreach ($Root in $Roots) {
      $Base = Join-Path $Root 'VC/Redist/MSVC'
      if (Test-Path -LiteralPath $Base -PathType Container) {
        $Redist += Get-ChildItem -LiteralPath $Base -Directory |
          Where-Object Name -Match '^\d+\.\d+\.\d+$' |
          Sort-Object { [version]$_.Name } -Descending | Select-Object -ExpandProperty FullName
      }
    }
  }
  foreach ($Root in $Redist) {
    $Arch = Join-Path $Root 'x64'
    if (-not (Test-Path -LiteralPath $Arch -PathType Container)) { continue }
    foreach ($Directory in Get-ChildItem -LiteralPath $Arch -Directory -Filter 'Microsoft.VC*.CRT') {
      if ((Test-Path -LiteralPath (Join-Path $Directory.FullName 'vcruntime140.dll')) -and
          (Test-Path -LiteralPath (Join-Path $Directory.FullName 'vcruntime140_1.dll'))) {
        return $Directory.FullName
      }
    }
  }
  throw 'Visual C++ x64 redistributable files were not found. Install the Windows Desktop development with C++ workload and the current MSVC x64/x86 tools.'
}

function Copy-NoosphereCrt {
  param([string]$Source, [string]$Destination)
  foreach ($Dll in Get-ChildItem -LiteralPath $Source -File -Filter '*.dll') {
    $Signature = Get-AuthenticodeSignature -LiteralPath $Dll.FullName
    if ($Signature.Status -ne 'Valid' -or $Signature.SignerCertificate.Subject -notmatch 'Microsoft Corporation') {
      throw "Invalid Microsoft signature for CRT file $($Dll.FullName)."
    }
    Copy-Item -LiteralPath $Dll.FullName -Destination $Destination -Force
  }
}

function Copy-NoosphereCodeLensRuntime {
  param([string]$Source, [string]$DestinationRoot)
  $DestinationRoot = [IO.Path]::GetFullPath($DestinationRoot)
  $Destination = [IO.Path]::GetFullPath((Join-Path $DestinationRoot 'codelens-runtime'))
  $Boundary = $DestinationRoot.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
  if (-not $Destination.StartsWith($Boundary, [StringComparison]::OrdinalIgnoreCase)) { throw 'Invalid CodeLens staging path.' }
  $Stage = Join-Path $DestinationRoot ('.codelens-stage-' + [guid]::NewGuid().ToString('N'))
  Copy-Item -LiteralPath $Source -Destination $Stage -Recurse
  if (Test-Path -LiteralPath $Destination) {
    $Item = Get-Item -LiteralPath $Destination -Force
    if (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'CodeLens distribution directory cannot be a link.' }
    Remove-Item -LiteralPath $Destination -Recurse -Force
  }
  Move-Item -LiteralPath $Stage -Destination $Destination
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

function Get-NoosphereSourceIdentity {
  param([string]$ProjectRoot)
  $Git = Get-Command git -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
  if (-not $Git) { return @{ revision = $null; dirty = $null; fingerprint = $null } }
  $Revision = & $Git.Source -C $ProjectRoot rev-parse HEAD 2>$null
  if ($LASTEXITCODE -ne 0) { return @{ revision = $null; dirty = $null; fingerprint = $null } }
  $Changes = @(& $Git.Source -C $ProjectRoot status --porcelain --untracked-files=normal 2>$null)
  if ($LASTEXITCODE -ne 0) { throw 'Could not inspect source changes for the build manifest.' }
  # Hash actual build inputs, including unstaged and untracked nonignored files.
  # Documentation and private ignored runtime/build output do not affect this scope.
  $Listed = @(& $Git.Source -C $ProjectRoot ls-files -co --exclude-standard -z 2>$null)
  if ($LASTEXITCODE -ne 0) { throw 'Could not enumerate production source for the build manifest.' }
  [string[]]$Entries = @((($Listed -join "`n") -split "`0") | Where-Object {
    $_ -match '^(backend/|frontend/|desktop/|scripts/|\.cargo/|VERSION$|\.node-version$|\.python-version$|rust-toolchain\.toml$|setup\.bat$|\.gitignore$|\.gitattributes$)'
  } | Select-Object -Unique | ForEach-Object {
    $Path = Join-Path $ProjectRoot $_
    $Hash = if (Test-Path -LiteralPath $Path -PathType Leaf) { (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash } else { 'MISSING' }
    $_ + "`t" + $Hash
  })
  [Array]::Sort($Entries, [StringComparer]::Ordinal)
  $Hasher = [Security.Cryptography.SHA256]::Create()
  try { $Fingerprint = [BitConverter]::ToString($Hasher.ComputeHash([Text.Encoding]::UTF8.GetBytes(($Entries -join "`n")))).Replace('-', '') }
  finally { $Hasher.Dispose() }
  return @{ revision = ([string]$Revision).Trim(); dirty = ($Changes.Count -gt 0); fingerprint = $Fingerprint }
}

function Get-NoosphereFrontendHash {
  param([string]$ProjectRoot)
  $Directory = [IO.Path]::GetFullPath((Join-Path $ProjectRoot 'frontend/dist'))
  if (-not (Test-Path -LiteralPath (Join-Path $Directory 'index.html') -PathType Leaf)) { throw 'frontend/dist/index.html is missing.' }
  [string[]]$Entries = @(Get-ChildItem -LiteralPath $Directory -Recurse -File | ForEach-Object {
    $Relative = $_.FullName.Substring($Directory.Length).TrimStart([char[]]@('\', '/')).Replace('\', '/')
    $Relative + "`t" + (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash
  })
  [Array]::Sort($Entries, [StringComparer]::Ordinal)
  $Hasher = [Security.Cryptography.SHA256]::Create()
  try { return [BitConverter]::ToString($Hasher.ComputeHash([Text.Encoding]::UTF8.GetBytes(($Entries -join "`n")))).Replace('-', '') }
  finally { $Hasher.Dispose() }
}

function Get-NoosphereReleaseInfo {
  param([string]$ReleaseRoot, [string]$ExpectedSourceRevision, [string]$ExpectedSourceFingerprint)
  $ReleaseRoot = [IO.Path]::GetFullPath($ReleaseRoot)
  foreach ($Relative in @('Noosphere.exe', 'bin/noosphere-backend.exe', 'codelens-python/main.py', 'config.defaults.yaml', 'build-info.json')) {
    if (-not (Test-Path -LiteralPath (Join-Path $ReleaseRoot $Relative) -PathType Leaf)) { throw "Portable release is incomplete: $Relative. Run scripts/build.ps1 first." }
  }
  $Info = Get-Content -LiteralPath (Join-Path $ReleaseRoot 'build-info.json') -Raw | ConvertFrom-Json
  if ($Info.rea_runtime) {
    foreach ($Relative in @('rea-runtime/node.exe', 'rea-runtime/NODE-LICENSE.txt', 'rea-runtime/node_modules/rea-agents/scripts/rea.mjs', 'rea-runtime/node_modules/rea-agents/LICENSE', 'rea-runtime/distribution.json')) {
      if (-not (Test-Path -LiteralPath (Join-Path $ReleaseRoot $Relative) -PathType Leaf)) { throw "REA 分发组件缺失 $Relative。" }
    }
    $ReaInfo = Get-Content -LiteralPath (Join-Path $ReleaseRoot 'rea-runtime/distribution.json') -Raw | ConvertFrom-Json
    if ($ReaInfo.rea -ne $Info.rea_runtime.rea -or $ReaInfo.node -ne $Info.rea_runtime.node -or $ReaInfo.lock_sha256 -ne $Info.rea_runtime.lock_sha256) { throw 'REA 分发清单与构建记录不匹配。' }
  }
  if ($Info.codelens_runtime) {
    foreach ($Relative in @('codelens-runtime/python.exe', 'codelens-runtime/python314.dll', 'codelens-runtime/python314.zip',
        'codelens-runtime/distribution.json', 'vcruntime140.dll', 'vcruntime140_1.dll')) {
      if (-not (Test-Path -LiteralPath (Join-Path $ReleaseRoot $Relative) -PathType Leaf)) {
        throw "Windows runtime resource is missing: $Relative."
      }
    }
    $RuntimeInfo = Get-Content -LiteralPath (Join-Path $ReleaseRoot 'codelens-runtime/distribution.json') -Raw | ConvertFrom-Json
    foreach ($Field in @('python', 'archive_sha256', 'lock_sha256')) {
      if ($Info.codelens_runtime.$Field -ne $RuntimeInfo.$Field) { throw "CodeLens runtime manifest differs at $Field." }
    }
  }
  if ($ExpectedSourceRevision -and $Info.source_revision -ne $ExpectedSourceRevision) { throw 'Release source revision differs from the requested build. Publish the current desktop build before refreshing shortcuts.' }
  if ($ExpectedSourceFingerprint -and $Info.source_fingerprint -ne $ExpectedSourceFingerprint) { throw 'Release source fingerprint differs from the current production source, including uncommitted changes. Rebuild and publish before refreshing shortcuts.' }
  foreach ($Pair in @(@('Noosphere.exe', 'desktop_sha256'), @('bin/noosphere-backend.exe', 'backend_sha256'))) {
    $Actual = (Get-FileHash -LiteralPath (Join-Path $ReleaseRoot $Pair[0]) -Algorithm SHA256).Hash
    if ($Info.($Pair[1]) -ne $Actual) { throw "Release manifest hash mismatch: $($Pair[0]). Publish a complete build before refreshing shortcuts." }
  }
  return $Info
}

function New-NoosphereShortcut {
  param([string]$LinkPath, [string]$ReleaseRoot, [string]$ExpectedSourceRevision, [string]$ExpectedSourceFingerprint)
  $ReleaseRoot = [IO.Path]::GetFullPath($ReleaseRoot)
  $LinkPath = [IO.Path]::GetFullPath($LinkPath)
  $null = Get-NoosphereReleaseInfo $ReleaseRoot $ExpectedSourceRevision $ExpectedSourceFingerprint
  $Executable = Join-Path $ReleaseRoot 'Noosphere.exe'
  $Shell = New-Object -ComObject WScript.Shell
  if (Test-Path -LiteralPath $LinkPath) {
    $Backup = $LinkPath + '.' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8) + '.bak'
    Copy-Item -LiteralPath $LinkPath -Destination $Backup
  }
  $Link = $Shell.CreateShortcut($LinkPath)
  $Link.TargetPath = $Executable
  $Link.WorkingDirectory = $ReleaseRoot
  $Link.Arguments = ''
  $Link.IconLocation = "$Executable,0"
  $Link.Description = 'Noosphere - Personal Intelligence Workspace'
  $Link.WindowStyle = 1
  $Link.Save()
  $Verified = $Shell.CreateShortcut($LinkPath)
  if ($Verified.TargetPath -ne $Executable -or $Verified.WorkingDirectory -ne $ReleaseRoot -or $Verified.Arguments -ne '') { throw "Shortcut read-back verification failed: $LinkPath" }
  Write-Host "Verified application shortcut: $LinkPath -> $Executable"
}

function Publish-NoosphereRelease {
  param(
    [string]$ProjectRoot, [string]$DesktopArtifact, [string]$BackendArtifact, [string[]]$InstallerArtifacts = @(),
    [string]$ReleaseRoot, [ValidateSet('release', 'dev')][string]$BuildProfile = 'release', [string]$ReaRuntime,
    [string]$CodeLensRuntime, [string]$CrtRuntime, [hashtable]$Toolchains, [switch]$PackageStage
  )
  $ProjectRoot = [IO.Path]::GetFullPath($ProjectRoot)
  if (-not $ReleaseRoot) { $ReleaseRoot = Join-Path $ProjectRoot 'release' }
  $ReleaseRoot = [IO.Path]::GetFullPath($ReleaseRoot)
  if ($ReleaseRoot.TrimEnd('\', '/') -eq [IO.Path]::GetPathRoot($ReleaseRoot).TrimEnd('\', '/') -or $ReleaseRoot -eq $ProjectRoot) { throw 'ReleaseRoot must be a dedicated portable application directory.' }
  foreach ($Artifact in (@($DesktopArtifact, $BackendArtifact) + $InstallerArtifacts)) {
    if (-not (Test-Path -LiteralPath $Artifact -PathType Leaf)) { throw "Build artifact is missing: $Artifact" }
  }
  $Source = Get-NoosphereSourceIdentity $ProjectRoot
  $FrontendHash = Get-NoosphereFrontendHash $ProjectRoot
  $ReleaseBin = Join-Path $ReleaseRoot 'bin'
  # Detect locked user executables before replacing either member of the desktop/backend pair.
  foreach ($Destination in @((Join-Path $ReleaseRoot 'Noosphere.exe'), (Join-Path $ReleaseBin 'noosphere-backend.exe'))) {
    if (Test-Path -LiteralPath $Destination -PathType Leaf) {
      try { $Handle = [IO.File]::Open($Destination, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None); $Handle.Dispose() }
      catch { throw "Cannot replace $Destination. Close the running Noosphere application gracefully and retry; no process was stopped. $($_.Exception.Message)" }
    }
  }
  New-Item -ItemType Directory -Path $ReleaseBin -Force | Out-Null
  $PreviousDesktop = Join-Path $ReleaseRoot 'Noosphere.exe'
  if (Test-Path -LiteralPath $PreviousDesktop -PathType Leaf) {
    $BackupRoot = Join-Path $ReleaseRoot ('legacy/desktop/' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Path $BackupRoot -Force | Out-Null
    Copy-Item -LiteralPath $PreviousDesktop -Destination (Join-Path $BackupRoot 'Noosphere.exe')
    $PreviousBackend = Join-Path $ReleaseBin 'noosphere-backend.exe'
    if (Test-Path -LiteralPath $PreviousBackend) { Copy-Item -LiteralPath $PreviousBackend -Destination (Join-Path $BackupRoot 'noosphere-backend.exe') }
    $PreviousInfo = Join-Path $ReleaseRoot 'build-info.json'
    if (Test-Path -LiteralPath $PreviousInfo) { Copy-Item -LiteralPath $PreviousInfo -Destination (Join-Path $BackupRoot 'build-info.json') }
  }
  Copy-Item -LiteralPath $DesktopArtifact -Destination (Join-Path $ReleaseRoot 'Noosphere.exe') -Force
  Copy-Item -LiteralPath $BackendArtifact -Destination (Join-Path $ReleaseBin 'noosphere-backend.exe') -Force
  Copy-Item -LiteralPath (Join-Path $ProjectRoot 'desktop/src-tauri/config.defaults.yaml') -Destination (Join-Path $ReleaseRoot 'config.defaults.yaml') -Force
  if (-not $PackageStage -and -not (Test-Path -LiteralPath (Join-Path $ReleaseRoot 'config.yaml'))) {
    Copy-Item -LiteralPath (Join-Path $ReleaseRoot 'config.defaults.yaml') -Destination (Join-Path $ReleaseRoot 'config.yaml')
  }
  if (-not $PackageStage -and -not (Test-Path -LiteralPath (Join-Path $ReleaseRoot 'models.yaml'))) {
    Write-NoosphereUtf8 (Join-Path $ReleaseRoot 'models.yaml') "default_model: `"`"`nproviders: []`n"
  }
  Copy-NoosphereCodeLens $ProjectRoot (Join-Path $ReleaseRoot 'codelens-python')
  if ($ReaRuntime) { Copy-NoosphereRea $ReaRuntime $ReleaseRoot }
  if ($CodeLensRuntime) { Copy-NoosphereCodeLensRuntime $CodeLensRuntime $ReleaseRoot }
  if ($CrtRuntime) { Copy-NoosphereCrt $CrtRuntime $ReleaseRoot }
  if (-not $PackageStage) { Copy-Item -LiteralPath (Join-Path $ProjectRoot 'setup.bat') -Destination (Join-Path $ReleaseRoot 'setup.bat') -Force }

  $OldInstallers = @(Get-ChildItem -LiteralPath $ReleaseRoot -Filter '*setup*.exe' -File)
  if ($OldInstallers.Count -gt 0) {
    $LegacyRoot = Join-Path $ReleaseRoot ('legacy/installers/' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Path $LegacyRoot -Force | Out-Null
    foreach ($OldInstaller in $OldInstallers) {
      $SourcePath = [IO.Path]::GetFullPath($OldInstaller.FullName)
      $DestinationPath = [IO.Path]::GetFullPath((Join-Path $LegacyRoot $OldInstaller.Name))
      $Boundary = $ReleaseRoot.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
      if (-not $SourcePath.StartsWith($Boundary, [StringComparison]::OrdinalIgnoreCase) -or -not $DestinationPath.StartsWith($Boundary, [StringComparison]::OrdinalIgnoreCase)) { throw 'Installer archive path escaped the release directory.' }
      Move-Item -LiteralPath $SourcePath -Destination $DestinationPath
    }
  }
  foreach ($Installer in $InstallerArtifacts) { Copy-Item -LiteralPath $Installer -Destination (Join-Path $ReleaseRoot (Split-Path -Leaf $Installer)) -Force }
  $BuildInfo = [ordered]@{
    version = Get-NoosphereVersion $ProjectRoot
    built_at_utc = [DateTime]::UtcNow.ToString('o')
    runtime_revision = $(if ($CodeLensRuntime) { 2 } else { 1 })
    source_revision = $Source.revision
    source_dirty = $Source.dirty
    source_fingerprint = $Source.fingerprint
    source_fingerprint_scope = 'production-v2:backend/,frontend/,desktop/,scripts/,.cargo/,VERSION,.node-version,.python-version,rust-toolchain.toml,setup.bat,.gitignore,.gitattributes;git-nonignored'
    build_profile = $BuildProfile
    frontend_sha256 = $FrontendHash
    desktop_sha256 = (Get-FileHash -LiteralPath (Join-Path $ReleaseRoot 'Noosphere.exe') -Algorithm SHA256).Hash
    backend_sha256 = (Get-FileHash -LiteralPath (Join-Path $ReleaseBin 'noosphere-backend.exe') -Algorithm SHA256).Hash
    rea_runtime = $(if ($ReaRuntime) { Get-Content -LiteralPath (Join-Path $ReleaseRoot 'rea-runtime/distribution.json') -Raw | ConvertFrom-Json })
    codelens_runtime = $(if ($CodeLensRuntime) { Get-Content -LiteralPath (Join-Path $ReleaseRoot 'codelens-runtime/distribution.json') -Raw | ConvertFrom-Json })
    toolchains = $Toolchains
    installers = @($InstallerArtifacts | ForEach-Object { Split-Path -Leaf $_ })
  }
  Write-NoosphereUtf8 (Join-Path $ReleaseRoot 'build-info.json') ($BuildInfo | ConvertTo-Json -Depth 3)
  $null = Get-NoosphereReleaseInfo $ReleaseRoot $Source.revision $Source.fingerprint
}
