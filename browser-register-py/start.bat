@echo off
chcp 65001 >nul
echo ========================================
echo   OpenAI 浏览器自动化注册 (旧版)
echo ========================================
echo.
echo [*] 检查依赖...
pip install -r requirements.txt -q
playwright install chromium -q
echo.
echo [*] 启动...
python main.py
pause
