@echo off
cd /d %~dp0
setlocal
set FAIL=0
echo WinMcpShim Test Suite
echo ===================================
echo.
echo --- shared ---
go test -count=1 -timeout 30s -v ./shared
if errorlevel 1 set FAIL=1
echo.
echo --- tools ---
go test -count=1 -timeout 60s -v ./tools
if errorlevel 1 set FAIL=1
echo.
echo --- shim ---
go test -count=1 -timeout 180s -v ./shim
if errorlevel 1 set FAIL=1
echo.
echo --- installer ---
go test -count=1 -timeout 30s -v ./installer
if errorlevel 1 set FAIL=1
echo.
echo --- strpatch ---
cd strpatch
go test -count=1 -timeout 30s -v .
if errorlevel 1 set /a FAIL=1
cd ..
echo.
echo ===================================
if %FAIL%==1 (
    echo SOME TESTS FAILED
    exit /b 1
) else (
    echo ALL TESTS PASSED
)
