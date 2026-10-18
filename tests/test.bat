@echo off
echo ========================================
echo   Noosphere Backend Test Suite
echo ========================================
echo.

set BASE=http://localhost:8080
set PASS=0
set FAIL=0

echo [1] Health Check
curl -s %BASE%/health | findstr "ok" >nul && (echo   PASS: health) || (echo   FAIL: health & set /a FAIL+=1)
set /a PASS+=1

echo [2] Gateway Health
curl -s %BASE%/api/gateway/health | findstr "ok" >nul && (echo   PASS: gateway) || (echo   FAIL: gateway & set /a FAIL+=1)
set /a PASS+=1

echo [3] Gateway Models
curl -s %BASE%/api/gateway/models | findstr "models" >nul && (echo   PASS: models) || (echo   FAIL: models & set /a FAIL+=1)
set /a PASS+=1

echo [4] Gateway Stats
curl -s %BASE%/api/gateway/stats | findstr "total" >nul && (echo   PASS: stats) || (echo   FAIL: stats & set /a FAIL+=1)
set /a PASS+=1

echo [5] Gateway Logs
curl -s %BASE%/api/gateway/logs | findstr "logs" >nul && (echo   PASS: logs) || (echo   FAIL: logs & set /a FAIL+=1)
set /a PASS+=1

echo [6] Add Model
curl -s -X POST %BASE%/api/gateway/models -H "Content-Type: application/json" -d "{\"id\":\"test\",\"name\":\"Test\",\"provider\":\"deepseek\",\"api_key\":\"sk-test\",\"model_name\":\"deepseek-v4-pro\"}" | findstr "added" >nul && (echo   PASS: add model) || (echo   FAIL: add model & set /a FAIL+=1)
set /a PASS+=1

echo [7] Delete Model
curl -s -X DELETE %BASE%/api/gateway/models/test | findstr "deleted" >nul && (echo   PASS: delete model) || (echo   FAIL: delete model & set /a FAIL+=1)
set /a PASS+=1

echo [8] Nebula API (proxy)
curl -s %BASE%/api/v1/stats | findstr "sessions" >nul && (echo   PASS: nebula proxy) || (echo   FAIL: nebula proxy & set /a FAIL+=1)
set /a PASS+=1

echo.
echo ========================================
echo   Results: %PASS% passed, %FAIL% failed
echo ========================================
