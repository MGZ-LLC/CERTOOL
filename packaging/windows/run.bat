@echo off
chcp 65001 >nul 2>&1
set "HERE=%~dp0"

REM Point the tool at the venv's python for the instrument bridge. The exe also
REM auto-detects .venv next to itself, so this is belt-and-braces.
if exist "%HERE%.venv\Scripts\python.exe" set "CERTOOL_PYTHON=%HERE%.venv\Scripts\python.exe"

if not exist "%CERTOOL_PYTHON%" (
    echo   No .venv found — run install.bat first / 请先运行 install.bat
    pause
    exit /b 1
)

echo   Starting certool ... a browser will open at http://127.0.0.1:8787/
"%HERE%certool.exe" %*
pause
