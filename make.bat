@echo off
cd /d %~dp0
if not exist bin mkdir bin
echo [1/7] Tidying dependencies...
go mod tidy || goto :fail
echo [2/7] Building winmcpshim.exe...
go build -o bin\winmcpshim.exe ./shim || goto :fail
echo [3/7] Building strpatch.exe...
cd strpatch
go build -o ..\bin\strpatch.exe . || goto :fail
cd ..
echo [4/7] Building install.exe...
go build -o bin\install.exe ./cmd/install || goto :fail
echo [5/7] Building uninstall.exe...
go build -o bin\uninstall.exe ./cmd/uninstall || goto :fail
echo [6/7] Building rogue.exe (test helper)...
go build -o bin\rogue.exe ./testhelpers/rogue || goto :fail
if exist shim.toml copy /y shim.toml bin\shim.toml >nul
echo [7/7] Packing MCPB bundle...
where npx >nul 2>&1 || (echo MCPB pack requires Node.js/npx on PATH. && goto :fail)
if not exist bundle mkdir bundle
copy /y bin\winmcpshim.exe bundle\winmcpshim.exe >nul || goto :fail
copy /y bin\strpatch.exe bundle\strpatch.exe >nul || goto :fail
copy /y manifest.json bundle\manifest.json >nul || goto :fail
copy /y README.md bundle\README.md >nul || goto :fail
copy /y LICENSE bundle\LICENSE >nul || goto :fail
copy /y PRIVACY.md bundle\PRIVACY.md >nul || goto :fail
copy /y config.ps1 bundle\config.ps1 >nul || goto :fail
copy /y config.cmd bundle\config.cmd >nul || goto :fail
copy /y icon\WinMcpShim.png bundle\icon.png >nul || goto :fail
for /f "delims=" %%v in ('powershell -NoProfile -Command "(ConvertFrom-Json (Get-Content -Raw manifest.json)).version"') do set MCPB_VERSION=%%v
if "%MCPB_VERSION%"=="" (echo Failed to read version from manifest.json && goto :fail)
call npx -y @anthropic-ai/mcpb pack bundle "winmcpshim-%MCPB_VERSION%.mcpb" || goto :fail
echo.
echo Build complete. Binaries in bin\, MCPB at winmcpshim-%MCPB_VERSION%.mcpb
goto :eof
:fail
echo BUILD FAILED
exit /b 1
