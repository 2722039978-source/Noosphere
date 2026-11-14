#Requires -Version 7.0
[CmdletBinding()]
param([ValidateSet('Frontend', 'Backend', 'Desktop', 'Package')][string]$Profile = 'Desktop',
      [switch]$ToolchainOnly)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
$Root = Split-Path -Parent $PSScriptRoot
$Failures = [Collections.Generic.List[string]]::new()

function Check-NoosphereRequirement([string]$Name, [scriptblock]$Check, [string]$Remedy) {
  try {
    $Detail = & $Check
    Write-Host "[OK] ${Name} $Detail"
  } catch {
    $Failures.Add("${Name} $($_.Exception.Message) $Remedy")
  }
}

function Read-NoosphereVersion([string]$File) {
  (Get-Content -LiteralPath (Join-Path $Root $File) -Raw).Trim()
}

function Require-NoosphereVersion([string]$Name, [string[]]$Arguments, [string]$Pattern) {
  $Command = Resolve-NoosphereCommand $Name
  $Output = (Invoke-NoosphereCommand $Command $Arguments $Root | Out-String).Trim()
  if ($Output -notmatch $Pattern) { throw "Unexpected version or target: $Output" }
  $Output
}

if ($Profile -ne 'Backend') {
  Check-NoosphereRequirement 'Node.js' {
    $Version = Read-NoosphereVersion '.node-version'
    Require-NoosphereVersion 'node' @('--version') ('^v' + [regex]::Escape($Version) + '$')
  } 'Install the version in .node-version, then reopen PowerShell.'
  Check-NoosphereRequirement 'npm' {
    Require-NoosphereVersion 'npm' @('--version') '^\d+\.\d+\.\d+'
  } 'Install npm with Node.js.'
  if (-not $ToolchainOnly) {
    Check-NoosphereRequirement 'Frontend dependencies' {
      if (-not (Test-Path -LiteralPath (Join-Path $Root 'frontend/node_modules/vite/bin/vite.js'))) {
        throw 'Vite is missing.'
      }
    } 'Run scripts/setup.ps1.'
  }
}
if ($Profile -ne 'Frontend') {
  Check-NoosphereRequirement 'Go' {
    $Line = Get-Content -LiteralPath (Join-Path $Root 'backend/go.mod') | Where-Object { $_ -match '^go ' }
    $Version = $Line.Substring(3).Trim()
    Require-NoosphereVersion 'go' @('version') ('\bgo' + [regex]::Escape($Version) + '\b.*windows/amd64')
  } 'Install the Windows x64 version in backend/go.mod.'
}
if ($Profile -in 'Desktop', 'Package') {
  Check-NoosphereRequirement 'Python' {
    $Version = Read-NoosphereVersion '.python-version'
    Require-NoosphereVersion 'python' @('--version') ('^Python ' + [regex]::Escape($Version) + '$')
    Invoke-NoosphereCommand (Resolve-NoosphereCommand 'python') @('-c', 'import struct; assert struct.calcsize("P") == 8') $Root | Out-Null
  } 'Install the x64 CPython version in .python-version or use the repository venv.'
  Check-NoosphereRequirement 'Rust MSVC' {
    $Version = [regex]::Match((Read-NoosphereVersion 'rust-toolchain.toml'), '(?m)^channel = "([^"]+)"').Groups[1].Value
    $Cargo = Resolve-NoosphereCommand 'cargo'
    $Info = Invoke-NoosphereCommand $Cargo @('-vV') $Root | Out-String
    if ($Info -notmatch ('(?m)^release: ' + [regex]::Escape($Version) + '\s*$') -or
        $Info -notmatch '(?m)^host: x86_64-pc-windows-msvc\s*$') { throw $Info.Trim() }
    "Cargo $Version (x86_64-pc-windows-msvc)"
  } 'Install rustup and the toolchain in rust-toolchain.toml. Use the MSVC host.'
  Check-NoosphereRequirement 'MSVC and Windows SDK' {
    $VsWhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio/Installer/vswhere.exe'
    if (-not (Test-Path -LiteralPath $VsWhere)) { throw 'vswhere was not found.' }
    $VS = & $VsWhere -products '*' -latest -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
    if (-not $VS) { throw 'The x64 MSVC component was not found.' }
    $Targets = @(Get-ChildItem -LiteralPath (Join-Path $VS 'VC/Tools/MSVC') -Directory |
      Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName 'bin/Hostx64/x64/link.exe') })
    if ($Targets.Count -eq 0) { throw 'The x64 linker was not found.' }
    $Kits = (Get-ItemProperty -LiteralPath 'HKLM:\SOFTWARE\Microsoft\Windows Kits\Installed Roots' -Name KitsRoot10).KitsRoot10
    $SDKs = @(Get-ChildItem -LiteralPath (Join-Path $Kits 'Lib') -Directory | Where-Object {
      (Test-Path -LiteralPath (Join-Path $_.FullName 'um/x64/kernel32.lib')) -and
      (Test-Path -LiteralPath (Join-Path $_.FullName 'ucrt/x64/ucrt.lib'))
    })
    if ($SDKs.Count -eq 0) { throw 'No usable Windows 10/11 x64 SDK was found.' }
    "$VS | SDK $($SDKs[-1].Name)"
  } 'In Visual Studio Installer, install Desktop development with C++ and a Windows SDK.'
  Check-NoosphereRequirement 'WebView2 Runtime' {
    $Application = Join-Path ${env:ProgramFiles(x86)} 'Microsoft/EdgeWebView/Application'
    $PerUser = Join-Path $env:LOCALAPPDATA 'Microsoft/EdgeWebView/Application'
    $Runtime = @(Get-ChildItem -Path "$Application/*/msedgewebview2.exe", "$PerUser/*/msedgewebview2.exe" -ErrorAction SilentlyContinue)
    if ($Runtime.Count -eq 0) { throw 'An Evergreen WebView2 Runtime executable was not found.' }
    $Runtime[0].VersionInfo.FileVersion
  } 'Install the Microsoft Edge WebView2 Runtime to run the desktop app and native tests.'
  if (-not $ToolchainOnly) {
    Check-NoosphereRequirement 'CodeLens Python packages' {
      $Python = Join-Path $Root '.local/venv/Scripts/python.exe'
      if (-not (Test-Path -LiteralPath $Python)) { throw 'The repository virtual environment is missing.' }
      Invoke-NoosphereCommand $Python @('-B', '-c', 'from ast_parser import get_analyzer; import main; assert len(get_analyzer().parsers) == 6') (Join-Path $Root 'backend/python/codelens') | Out-Null
    } 'Run scripts/setup.ps1 to create the repository venv and install CodeLens dependencies.'
    if ($Profile -eq 'Package') {
      Check-NoosphereRequirement 'Visual C++ redistributable files' {
        Find-NoosphereCrt
      } 'In Visual Studio Installer, add the x64 Visual C++ redistributable files.'
      Check-NoosphereRequirement 'Tauri CLI' {
        $Cargo = Resolve-NoosphereCommand 'cargo'
        $Cli = Resolve-NoosphereTauriCli $Root $Cargo
        Invoke-NoosphereCommand $Cli.Command ($Cli.Prefix + @('--version')) $Root
      } 'Run scripts/setup.ps1 to install desktop npm dependencies.'
    }
  }
}
if ($Failures.Count) { throw ($Failures -join [Environment]::NewLine) }
