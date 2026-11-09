@echo off
setlocal
if not exist "%~dp0release\Noosphere.exe" (
  echo Noosphere has not been built. Run scripts\build.ps1 first.
  pause
  exit /b 1
)
if not exist "%~dp0release\build-info.json" (
  echo Build metadata is missing. Run scripts\build.ps1 to build the application.
  echo See README.md for the build instructions.
  pause
  exit /b 1
)
start "" /D "%~dp0release" "%~dp0release\Noosphere.exe"
