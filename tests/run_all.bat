@echo off
echo ========================================
echo   Noosphere Test Suite
echo ========================================
echo.
set BASE=http://127.0.0.1:8080
set PASS=0
set FAIL=0

echo [1/8] Health Check
curl -s %BASE%/health | findstr "ok" >nul && (echo   PASS) || (echo   FAIL & set /a FAIL+=1)
set /a PASS+=1

echo [2/8] Gateway Health
curl -s %BASE%/api/gateway/health | findstr "ok" >nul && (echo   PASS) || (echo   FAIL & set /a FAIL+=1)
set /a PASS+=1

echo [3/8] Models List
curl -s %BASE%/api/gateway/models | findstr "models" >nul && (echo   PASS) || (echo   FAIL & set /a FAIL+=1)
set /a PASS+=1

echo [4/8] Stats
curl -s %BASE%/api/gateway/stats | findstr "total" >nul && (echo   PASS) || (echo   FAIL & set /a FAIL+=1)
set /a PASS+=1

echo [5/8] Logs
curl -s %BASE%/api/gateway/logs | findstr "logs" >nul && (echo   PASS) || (echo   FAIL & set /a FAIL+=1)
set /a PASS+=1

echo [6/8] Nebula Sessions
curl -s %BASE%/api/v1/stats | findstr "sessions" >nul && (echo   PASS) || (echo   FAIL & set /a FAIL+=1)
set /a PASS+=1

echo [7/8] DevOps Metrics
curl -s %BASE%/api/v1/devops/metrics | findstr "cpu" >nul && (echo   PASS) || (echo   FAIL & set /a FAIL+=1)
set /a PASS+=1

echo [8/8] DevOps Tools
curl -s %BASE%/api/v1/devops/tools | findstr "tools" >nul && (echo   PASS) || (echo   FAIL & set /a FAIL+=1)
set /a PASS+=1

echo.
echo ========================================
echo   Results: %PASS%/8 passed
echo ========================================
