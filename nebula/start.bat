@echo off
title Nebula Agent - Memory Engine
cd /d "%~dp0"

echo.
echo ============================================
echo    Nebula Agent - XingYun Memory Engine
echo    Make Every Conversation Remember
echo ============================================
echo.
echo    [*] Port: 8730
echo    [*] Dashboard: http://localhost:8730
echo.
echo    Press Ctrl+C to stop
echo ============================================
echo.

where go >nul 2>nul
if errorlevel 1 (
    echo    [X] ERROR: Go not found. Install from https://go.dev/dl/
    echo    [i] Or use Docker: docker compose up -d nebula
    pause
    exit /b 1
)

go run ./cmd/nebula-server --data ./nebula-data --port 8730

echo.
echo    [i] Server stopped
pause
