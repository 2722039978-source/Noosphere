# Recreate portable launch shortcuts after moving the project folder.
param([switch]$Desktop)
$ErrorActionPreference = 'Stop'
$ProjectRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$ReleaseRoot = Join-Path $ProjectRoot 'release'
$Executable = Join-Path $ReleaseRoot 'Noosphere.exe'
if (!(Test-Path -LiteralPath $Executable) -or !(Test-Path -LiteralPath (Join-Path $ReleaseRoot 'build-info.json'))) {
  throw 'Build the Windows desktop release with scripts/build.ps1 first.'
}
$ShortcutFolders = @($ProjectRoot)
if ($Desktop) { $ShortcutFolders += [Environment]::GetFolderPath('Desktop') }
$Shell = New-Object -ComObject WScript.Shell
foreach ($Folder in $ShortcutFolders | Select-Object -Unique) {
  $LinkPath = Join-Path $Folder 'Noosphere.lnk'
  if (Test-Path -LiteralPath $LinkPath) {
    $Existing = $Shell.CreateShortcut($LinkPath)
    if ($Existing.TargetPath -ne $Executable) {
      $Backup = $LinkPath + '.' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '.bak'
      Copy-Item -LiteralPath $LinkPath -Destination $Backup
    }
  }
  $Link = $Shell.CreateShortcut($LinkPath)
  $Link.TargetPath = $Executable
  $Link.WorkingDirectory = $ReleaseRoot
  $Link.IconLocation = "$Executable,0"
  $Link.Description = 'Noosphere - Personal Intelligence Workspace'
  $Link.WindowStyle = 1
  $Link.Save()
  Write-Host "Created desktop application shortcut: $LinkPath"
}
