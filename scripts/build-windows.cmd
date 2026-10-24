@echo off
rem Limit execution-policy bypass to the child process; do not change machine policy.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0build-windows.ps1" %*
exit /b %errorlevel%
