@echo off
title DevOps Agent - Ops Intelligence
cd /d "%~dp0"

echo.
echo ============================================
echo    DevOps Agent - Ops Intelligence
echo    Every Failure Becomes Experience
echo ============================================
echo.
echo    [*] Port: 8740
echo    [*] Dashboard: http://localhost:8740/web/
echo    [*] Memory: Persistent (./devops-memory)
echo.
echo    Press Ctrl+C to stop
echo ============================================
echo.

where go >nul 2>nul
if errorlevel 1 (
    echo    [X] ERROR: Go not found. Install from https://go.dev/dl/
    echo    [i] Or use Docker: docker compose up -d devops
    pause
    exit /b 1
)

go run ./cmd/devops-server --port 8740

echo.
echo    [i] Server stopped
pause
