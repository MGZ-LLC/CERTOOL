@echo off
chcp 65001 >nul 2>&1
echo.
echo   certool — instrument bridge setup / 仪器桥接安装
echo   =====================================================
echo.

REM --- Python present? ---
python --version >nul 2>&1 || (
    echo   Python not found / 找不到 Python
    echo   Install Python 3 from https://www.python.org/downloads/
    echo   TICK "Add Python to PATH" during install / 安装时请勾选 "Add Python to PATH"
    pause
    exit /b 1
)

set "VENV=%~dp0.venv"
if exist "%VENV%" (
    echo   Removing old environment / 删除旧的虚拟环境 ...
    rmdir /s /q "%VENV%"
)

echo   Creating virtual environment / 创建虚拟环境 ...
python -m venv "%VENV%" || ( echo   venv creation failed & pause & exit /b 1 )

echo   Installing dependencies / 安装依赖 ...
"%VENV%\Scripts\pip" install -r "%~dp0requirements.txt" || (
    echo.
    echo   Retry with China mirror / 使用清华镜像重试 ...
    "%VENV%\Scripts\pip" install -i https://pypi.tuna.tsinghua.edu.cn/simple -r "%~dp0requirements.txt" || (
        echo   Install failed — screenshot this / 安装失败，请截图 & pause & exit /b 1
    )
)

echo.
echo   Detecting instruments (VISA) / 检测仪器 ...
echo LIST | "%VENV%\Scripts\python" "%~dp0instr_helper.py"

echo.
echo   =====================================================
echo   Setup done. Run the tool with:  run.bat
echo.
echo   If NO instruments were listed above:
echo     - Easiest: install "Keysight IO Libraries Suite" (free) — it provides
echo       the VISA runtime + USBTMC drivers for BOTH the Keysight and Rigol,
echo       then re-run install.bat.
echo     - Or (advanced) bind each instrument to WinUSB with Zadig — see README.txt.
echo   =====================================================
echo.
pause
