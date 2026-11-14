function Initialize-NoosphereCodeLensRuntime {
  param([string]$ProjectRoot, [string]$PythonCommand)

  $Version = (Get-Content -LiteralPath (Join-Path $ProjectRoot '.python-version') -Raw).Trim()
  $Archives = @{
    '3.14.7' = 'D297E5FF019966817AD8502465176139F2D3D840FA4ED84B13BED399A6AB1F15'
  }
  if (-not $Archives.ContainsKey($Version)) { throw "No verified Windows x64 CPython archive is pinned for $Version." }
  $HostVersion = (Invoke-NoosphereCommand $PythonCommand @('-c', 'import platform; print(platform.python_version(), platform.machine())') $ProjectRoot | Out-String).Trim()
  if ($HostVersion -ne "$Version AMD64") { throw "CodeLens packaging needs Windows x64 Python $Version. Found $HostVersion." }

  $Recipe = Join-Path $ProjectRoot 'scripts/codelens-runtime'
  $Requirements = Join-Path $Recipe 'requirements-win-py314.lock'
  foreach ($Pin in Get-Content -LiteralPath (Join-Path $ProjectRoot 'backend/python/codelens/requirements.txt')) {
    $Pin = $Pin.Trim()
    if ($Pin -and -not $Pin.StartsWith('#') -and
        -not (Select-String -LiteralPath $Requirements -Pattern ('^' + [regex]::Escape($Pin) + '\s') -Quiet)) {
      throw "CodeLens lock does not contain $Pin. Resolve the Windows x64 wheel lock again."
    }
  }
  $LockHash = (Get-FileHash -LiteralPath $Requirements -Algorithm SHA256).Hash
  $Cache = Join-Path $ProjectRoot 'test/cache/codelens-runtime'
  $ArchiveName = "python-$Version-embed-amd64.zip"
  $Zip = Join-Path $Cache $ArchiveName
  $Prepared = Join-Path $Cache "$Version-$LockHash"
  New-Item -ItemType Directory -Path $Cache -Force | Out-Null
  $Lock = [IO.File]::Open((Join-Path $Cache 'prepare.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
  try {
    if (-not (Test-Path -LiteralPath (Join-Path $Prepared 'distribution.json') -PathType Leaf)) {
      if (-not (Test-Path -LiteralPath $Zip -PathType Leaf)) {
        $Download = $Zip + '.download'
        Invoke-WebRequest -Uri "https://www.python.org/ftp/python/$Version/$ArchiveName" -OutFile $Download
        if ((Get-FileHash -LiteralPath $Download -Algorithm SHA256).Hash -ne $Archives[$Version]) {
          throw 'CPython embedded archive SHA-256 did not match the pinned value.'
        }
        Move-Item -LiteralPath $Download -Destination $Zip
      }
      if ((Get-FileHash -LiteralPath $Zip -Algorithm SHA256).Hash -ne $Archives[$Version]) {
        throw "Invalid cached CPython archive $Zip. Remove the damaged archive and retry."
      }
      $Stage = Join-Path $Cache ("prepare-" + [guid]::NewGuid().ToString('N'))
      [IO.Compression.ZipFile]::ExtractToDirectory($Zip, $Stage)
      $SitePackages = Join-Path $Stage 'site-packages'
      Invoke-NoosphereCommand $PythonCommand @('-m', 'pip', '--isolated', '--disable-pip-version-check', 'install',
        '--only-binary=:all:', '--require-hashes', '--no-compile', '--target', $SitePackages, '-r', $Requirements) $ProjectRoot | Out-Host
      # The isolated interpreter ignores registry search paths, PYTHONPATH and user site packages.
      Write-NoosphereUtf8 (Join-Path $Stage 'python314._pth') "python314.zip`n.`nsite-packages`n../codelens-python`n"
      Write-NoosphereUtf8 (Join-Path $Stage 'distribution.json') (@{
        python = $Version
        archive_sha256 = $Archives[$Version]
        lock_sha256 = $LockHash
      } | ConvertTo-Json -Compress)
      Move-Item -LiteralPath $Stage -Destination $Prepared
    }
    return $Prepared
  } finally { $Lock.Dispose() }
}
