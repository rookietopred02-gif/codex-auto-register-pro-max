# OpenAI Auto Register

[![Python 3.8+](https://img.shields.io/badge/python-3.8+-blue.svg)](https://www.python.org/downloads/)
[![Playwright](https://img.shields.io/badge/Playwright-Async-green)](https://playwright.dev/python/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

基于 Playwright 的 OpenAI 账号自动注册工具。通过 IMAP 协议自动获取邮箱验证码，自动完成 Codex OAuth 授权流程并提取 `access_token` 与 `refresh_token`。无代理池功能，仅供单 IP 运行测试。已在 Windows、Alibaba Cloud Linux 3、Debian 12 上通过长时间循环测试。**仅供学习交流，请勿滥用。**

---

## 功能概览

- Token 获取机制：内置本地 Callback 服务器，全自动完成复杂的 Codex OAuth 授权链路（基于 PKCE 协议）。跳过网页版层层加密限制，直接通过回调拦截提取的 `access_token` 与 `refresh_token`。
- 通过 IMAP 自动读取验证码（支持 Gmail、QQ 邮箱及 Catch-all 泛域名邮箱）
- 内置浏览器指纹伪装，并且模拟真人输入和点击逻辑，降低被风控拦截的概率
- 多轮注册支持（可配置次数或无限循环），每轮可设置隔离时间
- 遇到页面超时、Cloudflare 盾牌卡死、按钮遮挡，还是邮件验证码接收延迟，底层全部捕获异常并执行安全退出。在多轮/无限挂机模式下，遇到死局会自动清理当前轮次并满血重启下一轮，真正实现无人值守。

---

## 环境要求

- Python >= 3.8
- Playwright（Chromium 内核）
- 一个可用的网络环境
- 一个可用的域名（用于 Catch-all 模式生成随机前缀邮箱）
- 一个支持 IMAP 的真实主邮箱（用于接收域名转发过来的 OpenAI 验证码）


---

## 安装

```bash
# 克隆仓库
git clone https://github.com/YourUsername/OpenAI-Auto-Register.git
cd OpenAI-Auto-Register

# 安装 Python 依赖
pip install -r requirements.txt

# 安装浏览器内核
playwright install chromium
```

> **Linux 用户**：还需要安装虚拟显示器（Xvfb）和系统底层渲染库，否则浏览器无法启动。
> 详细步骤可参考 [Alibaba Cloud Linux](./DEPLOY_LINUX.md) 或 [Debian 12](./DEPLOY_DEBIAN.md)，因为Linux系统众多，未能测试所有，其他系统使用方法可询问ai。

---

## 配置

项目提供了 `config.template.json` 作为配置模板，使用前需先复制并重命名：

```bash
# Windows
copy config.template.json config.json

# Linux / macOS
cp config.template.json config.json
```

然后编辑 `config.json`，填入你自己的信息：

| 参数 | 类型 | 说明 |
|------|------|------|
| `domain` | string | 注册邮箱的域名后缀，配合 `email_prefix` 生成随机邮箱地址 |
| `imap_host` | string | IMAP 服务器地址 |
| `imap_port` | int | IMAP 端口，通常为 `993` |
| `imap_user` | string | 接收验证码的邮箱账号 |
| `imap_pass` | string | 邮箱的 IMAP 授权码（非登录密码） |
| `email_prefix` | string | 注册邮箱前缀，生成格式为 `{prefix}XXXXX@domain`，例如 `auto` → `auto12345@domain.com`，留空则仅数字 |
| `run_count` | int | 注册轮数，`0` 表示无限循环 |
| `run_interval` | int | 每轮之间的间隔（秒），`0` 表示不等待 |
| `token_dir` | string | Token 文件保存目录 |
| `headless` | bool | 是否启用伪无头模式（⚠️ 注意：为绕过 Cloudflare 检测，即使设为 `true`，底层也是通过偏离坐标的“伪无头”全界面运行；Linux 环境下无论如何必须使用 `xvfb-run` 提供虚拟显示器） |
| `log_enabled` | bool | 选 `true` 时保存日志至 `log_dir` 防止查错，选 `false` 完全静默防爆盘 |
| `log_dir` | string | 日志文件保存目录 |
| `proxy` | string | **(选填)** 配置全局代理，例如 `socks5://127.0.0.1:1080` 或 `http://user:pass@ip:port` |

---

## 使用方法

### Windows / 带桌面环境的系统
```bash
python main.py
```

### Debian 12
> **注意**：在 Linux 服务器上可通过 `xvfb-run` 创建虚拟显示器来运行（这是规避风控的核心机制）。如果使用 Debian 12 等强限制系统，请务必先进入您的 `venv` 虚拟环境。

#### 方法 1：前台运行（关闭 SSH 则脚本停止）
```bash
xvfb-run --server-args="-screen 0 1920x1080x24" python main.py
```

#### 方法 2：使用 screen 后台挂机（强烈推荐）
使用 screen 可以在断开 SSH 连接后保持脚本运行，且不产生任何磁盘日志积压。
```bash
# 1. 创建一个名为 gpt 的新会话 
screen -S gpt

# 2. 在该会话内执行脚本
xvfb-run --server-args="-screen 0 1920x1080x24" python main.py

# 3. 退出并挂起会话：依次按下 Ctrl + A，然后按 D
# 4. 下次重连随时查看进度：
screen -r gpt
```

#### 方法 3：使用 nohup 挂机（直接屏蔽输出）
如果不习惯 screen，可以使用此方法将其完全打散到后台，并且将输出丢弃以防磁盘爆炸：
```bash
nohup xvfb-run --server-args="-screen 0 1920x1080x24" python main.py > /dev/null 2>&1 &
```

---

## 输出

注册成功后，脚本会在 `tokens/` 目录下生成 JSON 文件，包含：

```json
{
    "type": "codex",
    "email": "auto12345@example.com",
    "access_token": "eyJhbGciOi...",
    "refresh_token": "v1|abc123...",
    "expires_in": 86400,
    "saved_at": "2026-02-21T12:00:00+0800"
}
```

---

## 项目结构

```
.
├── main.py                    # 主脚本
├── config.template.json       # 配置模板（复制并重命名为 config.json 后使用）
├── requirements.txt           # Python 依赖
├── DEPLOY_LINUX.md            # Alibaba Cloud Linux 部署指南
├── DEPLOY_DEBIAN.md           # Debian 12 部署指南
├── tokens/                    # Token 输出目录（运行后自动生成，已排除上传）
└── logs/                      # 日志目录（log_enabled 为 true 时生成，已排除上传）
```

---

## 常见问题

**Q: 脚本运行后卡住不动？**
A: 大概率是缺少系统渲染库。在 Linux 上请执行 `playwright install-deps`，详见部署指南或询问AI。

**Q: 获取不到验证码？**
A: 检查你域名邮箱所转发的的邮箱是否支持在当前网络环境提供服务

**Q: 验证码总是不对？**
A: 检查你的 IMAP 邮箱是否为 Catch-all 配置。脚本会在验证码错误后尝试继续获取。

**Q: 日志文件占满磁盘？**
A: 将 `config.json` 中 `log_enabled` 设为 `false`。后台运行时建议使用 `screen` 或将输出重定向到 `/dev/null`。

**Q: 点击"继续"按钮后页面不跳转？**
A: 脚本内置了 6 秒等待 + 强制导航的兜底逻辑，通常会自动处理。如果仍然卡住，可能是网络风控值较高，超时会自动退出开始下一轮，欢迎提 Issue。

---

## 免责声明

- 本项目仅用于学习 Playwright 浏览器自动化技术，请勿用于违反 OpenAI 服务条款的行为。
- 使用本脚本所产生的一切后果（包括但不限于账号封禁、IP 限制等）由使用者自行承担。
- 请合理设置运行频率，避免对目标服务造成不必要的压力。
---

## 贡献

欢迎通过 Issue 反馈问题或提交 Pull Request。
