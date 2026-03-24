@echo off
chcp 65001 >nul
echo ========================================
echo   OpenAI 协议注册 - Python Web 控制台
echo ========================================
echo.

:: 检查 Python
where python >nul 2>&1
if errorlevel 1 (
    echo [错误] 未找到 Python，请先安装 Python 3.10+
    pause
    exit /b 1
)

:: 安装依赖
echo [*] 检查依赖...
pip install -r requirements.txt -q

echo.
echo [*] 启动 Web 服务器...
echo [*] 浏览器访问: http://localhost:8899
echo.
python web_server.py

pause
