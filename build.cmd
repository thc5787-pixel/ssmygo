@echo off
rem Builds ssmygo.exe; arguments go to build.ps1 (e.g. build -SkipTests, build -Arch all).
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*
set code=%ERRORLEVEL%
if not "%code%"=="0" if "%~1"=="" pause
exit /b %code%
