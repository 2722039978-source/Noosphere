@echo off
setlocal
if not exist "%~dp0release\Noosphere.exe" (
  echo Noosphere has not been built. Run scripts\build.ps1 first.
  pause
  exit /b 1
)
if not exist "%~dp0release\build-info.json" (
  echo This release predates the startup fix. Run a full scripts\build.ps1 build first.
  echo See docs\STARTUP_RELIABILITY.md for the current build status.
  pause
  exit /b 1
)
start "" /D "%~dp0release" "%~dp0release\Noosphere.exe"
