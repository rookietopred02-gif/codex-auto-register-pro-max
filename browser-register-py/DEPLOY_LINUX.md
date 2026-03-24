# OpenAI 账号注册脚本 Linux 服务器部署与挂机手册

本指南专为 **Alibaba Cloud Linux 3**  美国服务器优化，用于在无界面环境下稳定运行 Playwright 自动化脚本。

---

## 1. 环境初始化 (仅需执行一次)

在服务器终端依次执行以下模块。

### 1.1 安装系统底层环境与 Xvfb
```bash
sudo dnf update -y
sudo dnf install -y python3.11 python3.11-pip python3.11-devel xorg-x11-server-Xvfb libdrm mesa-libgbm pango at-spi2-atk gtk3 libX11 libXcomposite libXcursor libXdamage libXext libXi libXtst nss cups-libs libXrandr alsa-lib
```

### 1.2 安装 Python 依赖
使用 Python 3.11 确保与 Playwright 兼容：
```bash
# 升级 pip
python3.11 -m pip install --user --upgrade pip

# 安装核心依赖包
python3.11 -m pip install --user playwright playwright-stealth imap_tools httpx
```

### 1.3 安装浏览器内核
```bash
python3.11 -m playwright install chromium
```

---

## 2. 运行脚本方法

### 2.1 交互式运行 (用于调试查看日志)
进入项目目录后执行，这会启动虚拟桌面并运行脚本：
```bash
xvfb-run --server-args="-screen 0 1920x1080x24" python3.11 main.py
```

### 2.2 后台静默挂机 (SSH 断开不停止)
使用 `nohup` 命令将进程完全托管到后台，所有控制台输出将重定向到 `run.log`：
```bash
nohup xvfb-run --server-args="-screen 0 1920x1080x24" python3.11 main.py > run.log 2>&1 &
```

---

## 3. 运维常用命令

### 3.1 实时查看运行进度 (出号日志)
```bash
tail -f run.log
```

### 3.2 检查进程是否还在运行
```bash
ps -ef | grep main.py | grep -v grep
```
*   其中 `python3.11 main.py` 的 PID (第二列数字) 是实际控制脚本的 ID。

### 3.3 强制停止运行
```bash
# 优雅停止 (推荐)
pkill -f python3.11

# 强力清场 (杀掉所有 python 和虚拟显示器)
pkill -f python3.11 && pkill Xvfb
```

### 3.4 检查生成的 Token
```bash
ls -l ./tokens
```

---

## 4. 常见问题 (FAQ)

*   **报错 TargetClosedError**: 通常发生在手动杀掉了进程或系统资源极度匮乏导致浏览器崩溃时，重新运行后台命令即可。
*   **网络连接超时**: 虽然美国服务器无需代理，但若 IMAP 连接 Gmail 报错，请检查防火墙是否放行了 993 端口。
*   **权限不足**: 若命令提示 Permission Denied，请在命令前加上 `sudo`。
