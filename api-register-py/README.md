# api-register-py

OpenAI 账号注册工具 — **🛡️ 高稳定 Python 版**

> 采用 Python 实现，异常处理完善，自动重试，适合需要高可靠性的稳定跑量场景。  
> 失败立即重试，滚动窗口自动保护，遭遇异常不崩溃。

## 快速开始

**方式一：双击 `start.bat`（Windows，自动安装依赖）**

**方式二：命令行**

```bash
pip install -r requirements.txt
python web_server.py
```

然后浏览器打开 `http://localhost:8899`

## 依赖

```
curl_cffi >= 0.7.0
```

安装：
```bash
pip install -r requirements.txt
```

> `curl_cffi` 内置浏览器 TLS 指纹（模拟 Chrome/Firefox），无需安装 Playwright。

## 账号格式

每行一个账号：

```
# 格式一：Outlook 密码认证
邮箱----密码

# 格式二：Outlook XOAUTH2 认证（推荐）
邮箱----密码----client_id----refresh_token
```

## 参数说明

| 参数 | 说明 |
|------|------|
| 并发数 | 并发线程数，建议 3~10 |
| 代理 | HTTP 代理，如 `http://127.0.0.1:7890` |
| 注册转登录 | 已注册账号走登录流程刷新 Token |
| 域名邮箱 | 配置 catch-all IMAP 收件箱 |

## 与 Go 版的区别

| 对比项 | Go 版 | Python 版 |
|--------|-------|-----------|
| 运行方式 | 直接运行 exe，无需环境 | 需要 Python 3.10+ |
| 并发性能 | goroutine，更高效 | 多线程，稍慢 |
| XOAUTH2 | ✅ | ✅ |
| 域名邮箱 | ✅ 内置 IMAP 服务 | ✅ 内置 Hub |
| Web 控制台 | ✅ | ✅ |

## 结果目录

注册成功保存在 `tokens/<邮箱>.json`。
