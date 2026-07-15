@echo off
chcp 65001 >nul
title Noosphere Platform Launcher
cd /d "%~dp0"

echo.
echo ╔══════════════════════════════════════════════════════════╗
echo ║            Noosphere AI Platform                         ║
echo ║            An AI Operating Platform                      ║
echo ╚══════════════════════════════════════════════════════════╝
echo.
echo    Starting all services...
echo    Platform URL: http://localhost:3000
echo.

:: ============================================================
:: Check prerequisites
:: ============================================================
echo  ══ Pre-flight checks ══
echo.

:: Go
where go >nul 2>nul
if errorlevel 1 (
    echo    [WARN] Go not found - Nebula and DevOps will skip
    set "GO_OK=0"
) else (
    for /f "tokens=3" %%v in ('go version') do echo    [OK] Go %%v
    set "GO_OK=1"
)

:: Python
python --version >nul 2>nul
if errorlevel 1 (
    python3 --version >nul 2>nul
    if errorlevel 1 (
        py --version >nul 2>nul
        if errorlevel 1 (
            echo    [WARN] Python not found - CodeLens will skip
            set "PY_OK=0"
        ) else (
            set "PY_EXE=py"
            for /f "tokens=*" %%v in ('py --version') do echo    [OK] Python %%v
            set "PY_OK=1"
        )
    ) else (
        set "PY_EXE=python3"
        for /f "tokens=*" %%v in ('python3 --version') do echo    [OK] Python %%v
        set "PY_OK=1"
    )
) else (
    set "PY_EXE=python"
    for /f "tokens=*" %%v in ('python --version') do echo    [OK] Python %%v
    set "PY_OK=1"
)

:: Node.js (try multiple locations)
set "NODE_OK=0"
if exist "C:\Program Files\nodejs\node.exe" (
    set "PATH=C:\Program Files\nodejs;%APPDATA%\npm;%PATH%"
    set "NODE_OK=1"
    for /f "tokens=*" %%v in ('"C:\Program Files\nodejs\node.exe" --version') do echo    [OK] Node.js %%v
) else (
    where node >nul 2>nul
    if not errorlevel 1 (
        set "NODE_OK=1"
        for /f "tokens=*" %%v in ('node --version') do echo    [OK] Node.js %%v
    ) else (
        echo    [WARN] Node.js not found - Web frontend will skip
    )
)

:: Check if npm install was run
if "%NODE_OK%"=="1" (
    if not exist "web\node_modules" (
        echo    [INFO] Installing web dependencies (one time setup)...
        cd web
        call npm install
        cd ..
    )
)

echo.
echo  ══ Starting services ══
echo.
echo    Services will open in separate windows.
echo    Close each window to stop the corresponding service.
echo    Or close THIS window to stop ALL services.
echo.

:: ============================================================
:: Launch services
:: ============================================================

:: 1. Nebula (:8730) - Memory Engine
if "%GO_OK%"=="1" (
    echo    [1/4] Starting Nebula on :8730 ...
    start "Nebula Agent :8730" cmd /c "cd /d nebula && echo Nebula Agent - Memory Engine && echo Port: 8730 && echo. && go run ./cmd/nebula-server --data ./nebula-data --port 8730 && pause"
    timeout /t 2 >nul
)

:: 2. DevOps (:8740) - Ops Toolkit
if "%GO_OK%"=="1" (
    echo    [2/4] Starting DevOps on :8740 ...
    start "DevOps Agent :8740" cmd /c "cd /d devops && echo DevOps Agent - Ops Toolkit && echo Port: 8740 && echo. && go run ./cmd/devops-server --port 8740 && pause"
    timeout /t 2 >nul
)

:: 3. CodeLens (:8765) - Developer Intelligence
if "%PY_OK%"=="1" (
    echo    [3/4] Starting CodeLens on :8765 ...
    start "CodeLens AI :8765" cmd /c "cd /d codelens && echo CodeLens AI - Developer Intelligence && echo Port: 8765 && echo. && %PY_EXE% -m src.main serve && pause"
    timeout /t 3 >nul
)

:: 4. Web (:3000) - Platform Frontend
if "%NODE_OK%"=="1" (
    echo    [4/4] Starting Platform Frontend on :3000 ...
    start "Noosphere Platform :3000" cmd /c "cd /d web && set PATH=C:\Program Files\nodejs;%APPDATA%\npm;%PATH%; && echo Noosphere Platform Frontend && echo Port: 3000 && echo. && npx next dev -p 3000 && pause"
)

echo.
echo ╔══════════════════════════════════════════════════════════╗
echo ║  All services launched!                                 ║
echo ║                                                        ║
echo ║  Platform  → http://localhost:3000                      ║
echo ║  CodeLens  → http://localhost:8765                      ║
echo ║  Nebula    → http://localhost:8730                      ║
echo ║  DevOps    → http://localhost:8740/web/                 ║
echo ║                                                        ║
echo ║  Open http://localhost:3000 in your browser.            ║
echo ╚══════════════════════════════════════════════════════════╝
echo.
echo    Press any key to close this window.
echo    (Services will continue running in their own windows)
pause >nul
