@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"
title CodeLens AI

:: ============================================================
:: Find Python
:: ============================================================
set "PYTHON_EXE="
set "PIP_EXE="

python3 --version >nul 2>nul
if not errorlevel 1 (
    set "PYTHON_EXE=python3"
    set "PIP_EXE=pip3"
)

if not defined PYTHON_EXE (
    python --version >nul 2>nul
    if not errorlevel 1 (
        set "PYTHON_EXE=python"
        set "PIP_EXE=pip"
    )
)

if not defined PYTHON_EXE (
    py --version >nul 2>nul
    if not errorlevel 1 (
        set "PYTHON_EXE=py"
        set "PIP_EXE=py -m pip"
    )
)

if not defined PYTHON_EXE (
    echo [ERROR] Python not found.
    echo Please install Python 3.10+ from https://www.python.org/downloads/
    pause
    exit /b 1
)

echo [INFO] Python: %PYTHON_EXE%

:: Check for virtual environment
if exist "venv\Scripts\python.exe" (
    echo [INFO] Using virtual environment
    set "PYTHON_EXE=venv\Scripts\python.exe"
    set "PIP_EXE=venv\Scripts\pip.exe"
)

:: ============================================================
:: Check dependencies
:: ============================================================
echo [INFO] Checking dependencies...

%PYTHON_EXE% -c "import tree_sitter" >nul 2>nul
if errorlevel 1 (
    echo.
    echo [WARN] Dependencies not installed.
    echo.
    echo   [1] Full install (recommended)
    echo   [2] Minimal install (core only)
    echo   [3] Skip
    echo.
    set /p "INSTALL_MODE=Select option (1-3): "

    if "!INSTALL_MODE!"=="1" (
        echo [INFO] Installing all dependencies...
        call !PIP_EXE! install -r requirements.txt
        if errorlevel 1 (
            echo [WARN] Some packages failed to install. Core features may still work.
        )
    )
    if "!INSTALL_MODE!"=="2" (
        echo [INFO] Installing core dependencies...
        call !PIP_EXE! install tree-sitter networkx pyyaml rich loguru fastapi uvicorn pydantic click
    )
    if "!INSTALL_MODE!"=="3" (
        echo [INFO] Skipping install. Some features may not work.
    )
)

:: ============================================================
:: Main loop
:: ============================================================
:main_menu
cls
echo.
echo ============================================================
echo    CodeLens AI v1.0.0
echo    Intelligent Code Understanding Platform
echo ============================================================
echo.
echo    [1] Start Web Server
echo    [2] Index Project
echo    [3] Code Q and A
echo    [4] Trace Call Chain
echo    [5] Impact Analysis
echo    [6] Generate Docs
echo    [7] Git Diff Analysis
echo    [8] System Info
echo    [9] Run Tests
echo    [0] Exit
echo.
set /p "MODE=Select option (0-9): "

if "%MODE%"=="1" goto :serve
if "%MODE%"=="2" goto :index
if "%MODE%"=="3" goto :ask
if "%MODE%"=="4" goto :trace
if "%MODE%"=="5" goto :impact
if "%MODE%"=="6" goto :docs
if "%MODE%"=="7" goto :diff
if "%MODE%"=="8" goto :info
if "%MODE%"=="9" goto :test
if "%MODE%"=="0" goto :exit

echo Invalid option: %MODE%
pause
goto :main_menu

:: ============================================================
:: [1] Web Server
:: ============================================================
:serve
echo.
echo ============================================================
echo   Starting Web Server...
echo.
echo   Web UI:   http://localhost:8765
echo   API Docs: http://localhost:8765/docs
echo.
echo   Press Ctrl+C to stop
echo ============================================================
echo.

call %PYTHON_EXE% -m src.main serve
if errorlevel 1 (
    echo.
    echo [ERROR] Server stopped with error.
    pause
)
goto :main_menu

:: ============================================================
:: [2] Index Project
:: ============================================================
:index
echo.
set "PROJECT_PATH=."
set /p "PROJECT_PATH=Project path [%PROJECT_PATH%]: "
echo.
echo [INFO] Indexing: !PROJECT_PATH!
call %PYTHON_EXE% -m src.main index --project "!PROJECT_PATH!"
echo.
pause
goto :main_menu

:: ============================================================
:: [3] Code QA
:: ============================================================
:ask
echo.
set "ASK_PROJECT=."
set /p "ASK_PROJECT=Project path [%ASK_PROJECT%]: "
set /p "QUESTION=Your question: "
echo.
echo [INFO] Analyzing...
call %PYTHON_EXE% -m src.main ask --project "!ASK_PROJECT!" --question "!QUESTION!"
echo.
pause
goto :main_menu

:: ============================================================
:: [4] Trace Call Chain
:: ============================================================
:trace
echo.
set "TRACE_PROJECT=."
set /p "TRACE_PROJECT=Project path [%TRACE_PROJECT%]: "
set /p "ENTITY=Function/entity name: "
set "DEPTH=10"
set /p "DEPTH=Max depth [%DEPTH%]: "
echo.
echo [INFO] Tracing: !ENTITY!
call %PYTHON_EXE% -m src.main trace --project "!TRACE_PROJECT!" --entity "!ENTITY!" --depth !DEPTH!
echo.
pause
goto :main_menu

:: ============================================================
:: [5] Impact Analysis
:: ============================================================
:impact
echo.
set "IMPACT_PROJECT=."
set /p "IMPACT_PROJECT=Project path [%IMPACT_PROJECT%]: "
set /p "IMPACT_ENTITY=Entity name: "
echo.
echo [INFO] Analyzing impact: !IMPACT_ENTITY!
call %PYTHON_EXE% -m src.main impact --project "!IMPACT_PROJECT!" --entity "!IMPACT_ENTITY!"
echo.
pause
goto :main_menu

:: ============================================================
:: [6] Generate Docs
:: ============================================================
:docs
echo.
set "DOCS_PROJECT=."
set /p "DOCS_PROJECT=Project path [%DOCS_PROJECT%]: "
set "OUTPUT_PATH=./docs/PROJECT_DOCUMENTATION.md"
set /p "OUTPUT_PATH=Output path [%OUTPUT_PATH%]: "
echo.
echo [INFO] Generating docs...
call %PYTHON_EXE% -m src.main docs --project "!DOCS_PROJECT!" --output "!OUTPUT_PATH!"
echo.
pause
goto :main_menu

:: ============================================================
:: [7] Git Diff
:: ============================================================
:diff
echo.
set "DIFF_PROJECT=."
set /p "DIFF_PROJECT=Project path [%DIFF_PROJECT%]: "
echo.
echo   [1] Unstaged changes
echo   [2] Staged changes
echo   [3] Between branches
echo.
set /p "DIFF_MODE=Select (1-3): "

if "!DIFF_MODE!"=="1" (
    call %PYTHON_EXE% -m src.main diff --project "!DIFF_PROJECT!"
)
if "!DIFF_MODE!"=="2" (
    call %PYTHON_EXE% -m src.main diff --project "!DIFF_PROJECT!" --staged
)
if "!DIFF_MODE!"=="3" (
    set "BASE_BRANCH=main"
    set /p "BASE_BRANCH=Base branch [main]: "
    set /p "TARGET_BRANCH=Target branch: "
    call %PYTHON_EXE% -m src.main diff --project "!DIFF_PROJECT!" --base "!BASE_BRANCH!" --target "!TARGET_BRANCH!"
)
echo.
pause
goto :main_menu

:: ============================================================
:: [8] System Info
:: ============================================================
:info
echo.
call %PYTHON_EXE% -m src.main info
echo.
call %PYTHON_EXE% --version
echo.
pause
goto :main_menu

:: ============================================================
:: [9] Run Tests
:: ============================================================
:test
echo.
echo [INFO] Running tests...
call %PYTHON_EXE% -m pytest tests/ -v
if errorlevel 1 (
    echo.
    echo [WARN] pytest not found, trying unittest...
    call %PYTHON_EXE% -m unittest discover tests/ -v
)
echo.
pause
goto :main_menu

:: ============================================================
:: [0] Exit
:: ============================================================
:exit
echo.
echo Thank you for using CodeLens AI!
timeout /t 2 >nul
exit /b 0
