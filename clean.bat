@echo off
cd /d %~dp0
if exist bin rmdir /s /q bin
echo Clean.
