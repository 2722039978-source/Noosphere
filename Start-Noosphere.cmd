@echo off
setlocal
set "NoosphereRelease=%~dp0release"
if exist "%~dp0app\Noosphere.exe" set "NoosphereRelease=%~dp0app"
if exist "%~dp0release\app\Noosphere.exe" set "NoosphereRelease=%~dp0release\app"
if not exist "%NoosphereRelease%\Noosphere.exe" (
  echo Noosphere has not been built. Run scripts\build.ps1 first.
  pause
  exit /b 1
)
if not exist "%NoosphereRelease%\build-info.json" (
  echo Build metadata is missing. Run scripts\build.ps1 to build the application.
  echo See README.md for the build instructions.
  pause
  exit /b 1
)
start "" /D "%NoosphereRelease%" "%NoosphereRelease%\Noosphere.exe"
