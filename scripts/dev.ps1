# Run this project's Vite server and desktop app without requiring cargo-tauri.
# Frontend changes use HMR; restart this script after changing Rust code.
param([switch]$SkipBuild)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')
$ProjectRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$TauriRoot = Join-Path $ProjectRoot 'desktop/src-tauri'
$FrontendRoot = Join-Path $ProjectRoot 'frontend'
$CargoCommand = Resolve-NoosphereCommand 'cargo'
$NodeCommand = Resolve-NoosphereCommand 'node'
$ViteScript = Join-Path $FrontendRoot 'node_modules/vite/bin/vite.js'
if (-not (Test-Path -LiteralPath $ViteScript)) {
  throw "Frontend dependencies are missing. Run npm --prefix `"$FrontendRoot`" ci first."
}

if (-not $SkipBuild) {
  $GoCommand = Resolve-NoosphereCommand 'go'
  Build-NoosphereBackend $ProjectRoot $GoCommand (Get-NoosphereVersion $ProjectRoot) | Out-Null
} elseif (-not (Test-Path -LiteralPath (Join-Path $TauriRoot 'bin/noosphere-backend.exe'))) {
  throw '-SkipBuild requires desktop/src-tauri/bin/noosphere-backend.exe. Run without -SkipBuild once.'
}
Initialize-NoosphereResources $ProjectRoot

# Do not reuse an unrelated Vite server or stop a process this script did not start.
$PortProbe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 1420)
try { $PortProbe.Start() } catch { throw 'Port 1420 is in use. Stop the existing frontend server before running dev.ps1.' } finally { $PortProbe.Stop() }

$DevLogs = Join-Path $ProjectRoot '.local/dev-logs'
New-Item -ItemType Directory -Path $DevLogs -Force | Out-Null
$RunName = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
$ViteProcess = $null
try {
  $ViteProcess = Start-Process -FilePath $NodeCommand -ArgumentList @(('"' + $ViteScript + '"'), '--host', '127.0.0.1', '--port', '1420', '--strictPort') -WorkingDirectory $FrontendRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $DevLogs "$RunName-vite.stdout.log") -RedirectStandardError (Join-Path $DevLogs "$RunName-vite.stderr.log")
  $Ready = $false
  $Deadline = [DateTime]::UtcNow.AddSeconds(30)
  while ([DateTime]::UtcNow -lt $Deadline) {
    if ($ViteProcess.HasExited) { throw "Vite exited early. See $DevLogs" }
    try {
      $Response = Invoke-WebRequest -Uri 'http://127.0.0.1:1420' -UseBasicParsing -TimeoutSec 1
      if ($Response.StatusCode -eq 200) { $Ready = $true; break }
    } catch { Start-Sleep -Milliseconds 200 }
  }
  if (-not $Ready) { throw "Vite did not become ready on port 1420. See $DevLogs" }
  Write-Host 'Frontend ready. Starting desktop dev mode (close its window to stop).' -ForegroundColor Cyan
  Invoke-NoosphereCommand $CargoCommand @('run', '--locked', '--no-default-features', '--bin', 'noosphere-desktop') $TauriRoot
} finally {
  if ($null -ne $ViteProcess -and -not $ViteProcess.HasExited) {
    # Terminate only this newly created process tree, including Vite's esbuild child.
    & taskkill.exe /PID $ViteProcess.Id /T /F 2>$null | Out-Null
  }
  if ($null -ne $ViteProcess) { $ViteProcess.Dispose() }
}
