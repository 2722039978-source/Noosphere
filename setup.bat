@echo off
echo ========================================
echo   Noosphere CodeLens - Python Setup
echo ========================================
echo.
echo CodeLens requires Python 3.10+ with the following packages:
echo   - fastapi, uvicorn, pydantic, tree-sitter
echo.

rem --- locate a working Python 3 (flat goto chain: no nested if blocks,
rem     so %errorlevel% expands at run time, not at parse time) ---
where python >nul 2>&1
if %errorlevel% equ 0 (
    set PYTHON=python
    goto :check_version
)

where python3 >nul 2>&1
if %errorlevel% equ 0 (
    set PYTHON=python3
    goto :check_version
)

where py >nul 2>&1
if %errorlevel% neq 0 goto :no_python

py -3 --version >nul 2>&1
if %errorlevel% neq 0 goto :no_python
set PYTHON=py -3
goto :check_version

:no_python
echo Python not found on PATH.
echo.
echo Please install Python 3.10 or later:
echo   https://www.python.org/downloads/
echo.
echo Make sure to check "Add Python to PATH" during installation.
echo Then re-run this script.
pause
exit /b 1

:check_version
echo Found: %PYTHON%
%PYTHON% --version

echo.
echo Installing CodeLens dependencies...
rem --- support both layouts: source root (backend\python\codelens) or release (codelens-python) ---
cd /d "%~dp0"
if exist "codelens-python\requirements.txt" set "CODELENS_DIR=codelens-python"
if exist "backend\python\codelens\requirements.txt" set "CODELENS_DIR=backend\python\codelens"
if not defined CODELENS_DIR (
    echo ERROR: CodeLens Python files not found.
    pause
    exit /b 1
)
%PYTHON% -m pip install -r "%CODELENS_DIR%\requirements.txt"
if %errorlevel% equ 0 (
    echo.
    echo ========================================
    echo   Setup complete! CodeLens is ready.
    echo ========================================
) else (
    echo.
    echo ERROR: pip install failed. Check your network connection and Python installation.
)
pause
