# OpenAI 账号注册脚本 Debian 12 / Ubuntu 部署手册

在 Debian 12 (或更新版的 Ubuntu) 上，系统对 Python 环境的管理非常严格（默认禁止全局 `pip install`）。且 Playwright 运行必须安装一系列底层渲染依赖，否则**脚本运行后会直接卡死不动（浏览器进程在后台崩溃挂起）**。

请严格按照以下从零开始的步骤进行部署。

---

## 1. 安装系统基础环境

首先，更新系统源并安装 Python、虚拟环境管理工具以及虚拟显示器（Xvfb）。

```bash
sudo apt update
sudo apt install -y python3 python3-pip python3-venv xvfb
```

---

## 2. 初始化项目与虚拟环境 (关键步)

为了绕过 Debian 12 的环境变量限制，我们**必须**在项目根目录下创建一个虚拟环境（venv）。

```bash
# 1. 假设你的代码都在 /home/admin/gpt （或者你所在的任何存放 main.py 的目录）
cd /path/to/your/project

# 2. 创建虚拟环境 (这会在当前目录下生成一个名为 venv 的文件夹)
python3 -m venv venv

# 3. 激活虚拟环境 (激活后，你的命令行最前面会出现 (venv) 字样)
source venv/bin/activate
```
> **⚠️ 必须注意**：如果你关闭了终端或重启了服务器，下次进服务器跑脚本之前，**必须再次执行 `source venv/bin/activate`** 来激活它，否则命令会找不到。

---

## 3. 安装依赖包与浏览器核心 

此刻你处于 `(venv)` 状态下，所有的依赖都会被安全地隔离安装：

```bash
# 升级 pip 到最新版
pip install --upgrade pip

# 安装所有的 Python 第三方库
pip install playwright playwright-stealth imap_tools httpx

# 让 Playwright 自动下载 Chromium 浏览器
playwright install chromium

# ⚠️ 最导致卡死的罪魁祸首！⚠️
# 这条命令会让 Playwright 自动检查并用 apt 帮你安装系统缺失的几十个底层字体/渲染/C++库，不装它脚本必然卡死！
sudo playwright install-deps
```

---

## 4. 运行与挂机指南

运行方式和其它 Linux 系统一致，但**请确保你的终端正处于 `(venv)` 激活状态下**，由于在虚拟环境中，直接使用 `python` 命令即可。

### 4.1 普通运行 (观看日志情况)
```bash
xvfb-run --server-args="-screen 0 1920x1080x24" python main.py
```

### 4.2 后台长期挂机 (断开 SSH 继续跑)
```bash
nohup xvfb-run --server-args="-screen 0 1920x1080x24" python main.py > run.log 2>&1 &
```

---

## 5. 日常用法与维护

**看实时的运行日志：**
```bash
tail -f run.log
```

**如果要强行停止后台脚本：**
```bash
# 杀掉所有 python 进程与虚拟屏幕
pkill -f python && pkill Xvfb
```
