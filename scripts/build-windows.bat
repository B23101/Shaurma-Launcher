@echo off
rem Thin wrapper around build-windows.ps1. Contains NO secrets: tokens and keys are read
rem from environment variables (see the header of build-windows.ps1).
rem Usage: scripts\build-windows.bat [-Step components|installer|all] [-Edition shaurma|base|all]
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build-windows.ps1" %*
exit /b %ERRORLEVEL%
