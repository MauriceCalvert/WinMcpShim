@echo off
cd /d %~dp0
if not exist bin mkdir bin
echo Building winmcpshim.exe...
go build -o bin\winmcpshim.exe ./shim
if errorlevel 1 goto :fail
echo Running benchmark...
go run ./cmd/bench
goto :eof
:fail
echo BUILD FAILED
exit /b 1
