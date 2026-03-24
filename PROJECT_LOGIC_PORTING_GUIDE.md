# openai-auto-register-main 移植指南

这份文档不是项目概览，而是把当前仓库里已经实现的核心逻辑拆成可以移植的模块、状态机和时序，目标是让你把同一套逻辑搬到别的项目里。

默认以 `api-register-go` 为主，因为它是当前功能最完整、逻辑最新的一套实现。`api-register-py` 只作为补充参考。

---

## 1. 你真正需要搬的是什么

如果你的目标是“把这个项目的注册能力复制到另一个项目”，真正值得搬的不是某个页面或 Dashboard，而是下面 5 个模块：

1. 任务编排层
2. 注册状态机
3. OTP 总线
4. Temp Mail 资源层
5. 人工兜底桥接层

这 5 块在当前仓库里的对应关系如下：

| 模块 | 作用 | 当前实现 |
| --- | --- | --- |
| 任务编排层 | 解析输入、创建任务、并发执行、结果落盘 | `api-register-go/main.go` |
| 注册状态机 | 按 OpenAI 当前接口顺序完成注册 / 登录 / 换 token | `api-register-go/main.go` |
| OTP 总线 | 统一等待验证码、去重、消费、广播 | `api-register-go/imap_merged.go` |
| Temp Mail 资源层 | 申请临时邮箱、轮换、抓信、抽码、fallback | `api-register-go/temp_mail.go` |
| 人工兜底桥接层 | 手动输入 OTP / 手动输入临时邮箱 | `api-register-go/manual_code.go` |

你要移植时，最好也按这个边界拆，而不是直接把 `main.go` 整个复制过去。

---

## 2. 总体架构图

当前 Go 版的逻辑流可以压缩成下面这个结构：

```text
Dashboard / API 请求
    -> handleStart
        -> 账号列表 / Temp Mail 占位任务生成
        -> 可选配置 Integrated IMAP
        -> 配置 Temp Mail Runtime
        -> runWorkers
            -> doOne
                -> 可选 AcquireMailbox
                -> registerAccount
                    -> OAuth
                    -> Sentinel
                    -> Submit email
                    -> Submit register password / send OTP
                    -> waitForCode / waitForTempMailCode / fetchOutlookCode
                    -> verify OTP
                    -> create account
                    -> select workspace
                    -> follow redirect
                    -> exchange token
                -> 保存结果
                -> 可选延迟后切下一个账号
```

如果你要在别的项目里复用，最稳的方式是保留这条总链路不变，只把：

- UI
- 日志
- 结果落盘
- 外部配置加载

改成你自己的系统风格。

---

## 3. 任务编排层

---

## 3.1 输入模型

Go 版入口模型在 `api-register-go/main.go`：

- `Account`
- `DomainMailConfig`
- `StartRequest`
- `RegResult`

### 3.1.1 `Account`

最小必需字段：

- `Email`
- `Password`

可选字段：

- `ClientID`
- `RefreshToken`

这两个可选字段只在 Outlook IMAP 的 XOAUTH2 路径里有意义。

### 3.1.2 `StartRequest`

这个结构实际上就是一份“批量任务配置”：

- `Accounts`: 原始账号文本
- `Proxy`: 单任务代理
- `Workers`: 并发数
- `LoginMode`: 登录模式还是注册模式
- `SkipFinished`: 是否跳过已有结果
- `DomainMail`: 是否启用 catch-all IMAP
- `TempMail`: 是否启用临时邮箱模式

### 3.1.3 `TempMailConfig`

Temp Mail 模式的关键信息不多：

- `Count`: 要注册多少个账号
- `Password`: 新账号统一密码
- `AllowParallel`: 是否允许并发
- `NextDelaySeconds`: 一个账号成功后切换到下一个的延迟

移植时建议保留这个结构，不要把 Temp Mail 配置揉进普通注册配置里，否则后面逻辑会越来越乱。

---

## 3.2 任务创建

入口函数：`handleStart(...)`

这个函数的职责不是“注册”，而是构造任务运行上下文。

它做了 6 件事：

1. 防止已有任务运行时再次启动
2. 根据 `DomainMail` 自动配置集成 IMAP
3. 根据 `TempMail` 生成 placeholder 任务槽位
4. 普通模式下解析 `accounts` 文本
5. 可选过滤掉已完成账号
6. 启动 worker 池

### 3.2.1 Temp Mail 占位任务

Temp Mail 模式里不会直接把真实邮箱塞进队列，而是生成：

```text
temp-mail-1@placeholder.local
temp-mail-2@placeholder.local
...
```

这样设计的原因是：

- 前端只需要声明“我要 N 个账号”
- 实际邮箱地址可以在运行时动态分配
- 避免任务开始前一次性依赖邮箱服务

移植时，建议保留这种“placeholder task slot”设计。

### 3.2.2 跳过已完成账号

`SkipFinished` 逻辑通过扫描结果目录实现：

- 成功后按邮箱写 json
- 启动新任务时，如果发现该邮箱已有结果文件，则跳过

这不是数据库，但足够轻量。

如果你要接到新项目里：

- 小规模任务可继续用文件
- 中大型系统建议改成状态表

---

## 3.3 并发模型

入口：

- `runWorkers(...)`
- `doOne(...)`

### 3.3.1 当前实现

Go 版用的是信号量式并发控制：

```go
sem := make(chan struct{}, workers)
for each account:
    sem <- token
    go func() {
        defer <-sem
        doOne(...)
    }()
```

这个模型很适合移植，因为它：

- 简单
- 没有复杂调度器
- 任务互相独立
- 支持 stop flag

### 3.3.2 Temp Mail 并发保护

Temp Mail 模式下，并发数并不是完全照用户输入走，而是先经过：

- `normalizeTempWorkers(...)`

规则：

- 平行开关关闭时，强制 `workers = 1`
- 平行开关打开时，允许使用前端给定值
- 同时限制上下界

这样做是为了保护邮箱服务，不然一开跑就可能被 429。

如果你移植到别的项目，建议保留这个“业务保护层”，不要直接让 UI 输入决定实际并发。

---

## 3.4 单账号执行单元

`doOne(...)` 是整个系统最适合直接照抄的部分之一。

它做的事非常清晰：

1. 打印任务开始日志
2. 如果是 Temp Mail，先拿真实 mailbox
3. 如果拿 mailbox 失败，允许终端人工输入邮箱
4. 进入 `MaxRetry` 次注册尝试
5. 成功则保存结果
6. 如果是 Temp Mail，则按配置等待后切下一个账号
7. 如果失败，则记录失败结果

你可以把它理解成：

- 外层是一个任务槽位
- 内层是“获取资源 -> 执行业务 -> 提交结果”

移植时，这个函数建议继续保留为一个单独的 orchestrator，而不是把邮箱分配、注册和 OTP 等待揉成一个大函数。

---

## 4. HTTP 传输层

---

## 4.1 为什么它不是普通 `net/http`

Go 版没有直接用标准库，而是封装了 `HTTPClient`：

- `NewHTTPClient(proxy string)`
- `Get(...)`
- `PostJSON(...)`
- `PostForm(...)`
- `FollowRedirects(...)`

核心点：

- 随机 TLS 指纹
- 随机 User-Agent
- 固定请求头顺序
- 可选代理
- 本地 cookie 维护

这里的关键不是“写法漂亮”，而是它服务于两个目标：

1. 尽量模拟真实浏览器请求
2. 保证同一注册会话里 cookie 连续

如果你移植逻辑到别的项目，这层必须保留，不建议直接换回裸 `http.Client`，否则行为会变。

---

## 4.2 传输层需要具备的最小接口

如果你想重构成独立模块，建议把传输层抽成这个接口：

```go
type RegistrationTransport interface {
    Get(url string) (status int, body string, err error)
    PostJSON(url string, payload any, headers map[string]string) (status int, body string, err error)
    PostForm(url string, values url.Values) (status int, body string, err error)
    FollowRedirects(startURL string, maxHops int) (callbackURL string, err error)
    GetCookie(name string) string
}
```

然后由：

- Go 当前 `HTTPClient`
- 或你未来的新实现

去实现它。

这样主注册状态机就不会绑死在某个具体 HTTP 库上。

---

## 5. 注册状态机

这是整个项目里最关键的逻辑。

主入口：`registerAccount(...)`

你移植时，真正要复用的是“顺序”和“分支条件”，不是每一行代码。

---

## 5.1 状态机的主步骤

当前 Go 版是下面这个顺序：

### Step 1. OAuth 初始化

动作：

- `createOAuthParams()`
- `GET /oauth/authorize`
- 从 cookie 里取 `oai-did`

目的：

- 初始化会话
- 获取设备 ID
- 为后续 Sentinel 做准备

### Step 2. 获取 Sentinel token

动作：

- `POST https://sentinel.openai.com/backend-api/sentinel/req`

目的：

- 拿到后续 `authorize/continue` 所需的 sentinel header

如果你移植时漏掉这一步，后续请求很容易直接被拒。

### Step 3. 提交邮箱

动作：

- `POST /api/accounts/authorize/continue`

提交内容：

```json
{
  "username": { "value": email, "kind": "email" },
  "screen_hint": "signup"
}
```

关键点：

- `otpSentAt` 必须在这一步之前记录
- 因为对“已存在账号”而言，服务器可能在这一步后就自动发送 OTP

这是一个很重要的移植细节，漏了之后很容易误判新旧邮件。

### Step 4. 根据页面类型分支

当前 Go 版读的是 `page.type`。

最重要的几种类型：

- `create_account_password`
- `email_otp_send`
- `email_otp_verification`

#### 分支 A：`create_account_password`

这是当前最常见的注册路径。

必须执行：

- `GET continue_url`
- `POST /api/accounts/user/register`

提交：

```json
{
  "username": email,
  "password": password
}
```

成功后下一页通常变成：

- `email_otp_send`
- 或 `email_otp_verification`

这是目前项目里已经修过的关键逻辑。

不要再把这个状态接到旧的：

- `passwordless/send-otp`

#### 分支 B：`email_otp_verification`

说明：

- 服务器已经自动发出 OTP

此时不要再手动发送 OTP，而是直接进入等待验证码阶段。

#### 分支 C：其他页面

才会走旧式：

- `POST /api/accounts/passwordless/send-otp`

也就是说，是否“手动发 OTP”不是固定步骤，而是由当前页面类型决定。

这是移植时最容易搞错的地方之一。

---

## 5.2 OTP 获取阶段

根据配置，当前系统会从三个来源中选一个：

1. Temp Mail
2. Integrated IMAP
3. Outlook 独立 IMAP

选择条件：

- `tempMail != nil` -> Temp Mail
- `domainMail != nil` -> 集成 IMAP
- 其他 -> Outlook IMAP

这段决策应该保留在主状态机里，而不要让 OTP 模块自己猜来源。

---

## 5.3 验证 OTP

动作：

- `POST /api/accounts/email-otp/validate`

一旦成功，说明邮箱验证阶段结束。

这里有两个必须保留的注意点：

1. OTP 只能接受 `otpSentAt - 60s` 之后的邮件
2. OTP 获取成功后最好立即继续，不要再做很长的等待

---

## 5.4 创建账号

动作：

- 新账号才会执行 `POST /api/accounts/create_account`

提交：

- `name`
- `birthdate`

已存在账号或 login 模式则跳过。

这一步是否执行，不是看用户模式，而是同时看：

- `isExisting`
- `isLogin`

---

## 5.5 选择 workspace

动作：

- 从 `oai-client-auth-session` cookie 里解析出 workspace 列表
- 取第一个 workspace 的 id
- `POST /api/accounts/workspace/select`

然后拿到 `continue_url`。

这里的移植重点不是写法，而是认知：

- workspace 信息不一定来自 API 返回体
- 当前实现依赖 cookie 载荷

如果你移植到别的语言，必须保留：

- 读取 cookie
- base64 decode
- 提取 workspace id

---

## 5.6 跟随重定向并换 token

动作：

1. 跟随 `continue_url`
2. 直到拿到本地 callback URL
3. 从 query 中取 `code` 和 `state`
4. 校验返回 state 与初始 state 一致
5. `POST /oauth/token`

最后得到：

- `access_token`
- `refresh_token`
- `id_token`

这部分是注册流程真正的收尾。

移植时建议保留“显式跟随重定向”的实现，而不是依赖自动 redirect，因为当前逻辑需要自己截获 callback URL。

---

## 5.7 注册状态机伪代码

你可以直接把下面这段当作移植蓝本：

```text
function registerAccount(account, proxy, mode, domainMail, tempMail):
    email = account.email
    if tempMail placeholder:
        email = acquireTempMailbox()

    http = new HTTPClient(proxy)

    authURL, state, verifier = createOAuthParams()
    GET authURL
    deviceID = cookie["oai-did"]

    sentinel = POST sentinel(deviceID)

    otpSentAt = now()
    step3 = POST authorize_continue(email, sentinel)
    pageType = extractPageType(step3)

    if pageType == "create_account_password":
        GET step3.continue_url
        step4 = POST user_register(email, password)
        pageType = extractPageType(step4)
        if pageType in ["email_otp_send", "email_otp_verification"]:
            otpResendMode = "email_otp"
            otpSentAt = now()
    else if pageType == "email_otp_verification":
        otpResendMode = "email_otp"
    else:
        POST passwordless_send_otp()
        otpResendMode = "passwordless"
        otpSentAt = now()

    code = getOTP(email, otpSentAt, source=tempMail/domainMail/outlook, resendFn)
    POST validate_otp(code)

    if not existing and not loginMode:
        POST create_account(name, birthday)

    workspaceID = parseWorkspaceFromCookie()
    continueURL = POST select_workspace(workspaceID)
    callbackURL = followRedirects(continueURL)
    authCode = parseQuery(callbackURL, "code")
    assert parseQuery(callbackURL, "state") == state

    tokens = POST oauth_token(authCode, verifier)
    return tokens
```

---

## 6. OTP 总线

这是另一个值得完整移植的模块。

当前实现：`api-register-go/imap_merged.go`

核心对象：`IntegratedIMAPService`

这个服务的本质不是“IMAP 客户端”，而是一个 OTP 分发中心。

---

## 6.1 它保存什么状态

至少有这几类状态：

- 当前 IMAP 配置
- `codes[email] = IntegratedIMAPCode`
- `waiters[email] = []chan string`
- polling 是否已启动
- `stopCh`

这里最关键的是：

- `codes` 是当前已抓到、尚未消费的验证码缓存
- `waiters` 是正在等待某邮箱验证码的调用方

也就是说，这个服务同时承担了：

- cache
- pub/sub
- waiter registry

---

## 6.2 OTP 总线的最小接口

移植时建议抽成下面这组接口：

```go
type OTPBus interface {
    WaitForCode(email string, timeout time.Duration, minTime time.Time) (string, error)
    InjectCode(email, code, source string) error
    ConsumeCode(email, code string) error
    PeekCode(email string) (CodeEntry, bool)
}
```

然后：

- IMAP 轮询器向它注入 code
- Temp Mail 轮询器向它注入 code
- 手动输入也向它注入 code
- 注册主流程只调用 `WaitForCode`

这样模块边界会非常干净。

---

## 6.3 `WaitForCode(...)` 的设计

当前逻辑是：

1. 先查已有缓存，看是否已经有符合 `minTime` 的 code
2. 如果没有，则把当前等待者挂进 `waiters[email]`
3. 启动 timeout timer
4. 轮询 stop flag
5. 等待 `upsertCode(...)` 把 code 推进 channel

这个设计的优点是：

- 避免错过先到的验证码
- 不要求调用方自己轮询
- 多种来源可以统一投递

这是最值得保留的实现方式之一。

---

## 6.4 `upsertCode(...)` 的设计

它做了 4 件关键事：

1. 去重
2. 存入 `codes`
3. 广播到前端 / SSE
4. 唤醒对应邮箱的 waiter

而且一旦成功投递：

- 会删除该邮箱的 waiter 列表

这个设计保证同一封验证码不会被多个等待者无限重复消费。

---

## 6.5 为什么要有 `ConsumeCode(...)`

等待拿到 code 只是第一步。

`ConsumeCode(...)` 负责：

- 明确把某个邮箱对应的验证码从缓存中删掉
- 避免下次再次被用到

移植时千万不要只做 `WaitForCode` 而不做 `ConsumeCode`，否则旧码复用问题迟早会出现。

---

## 6.6 IMAP 轮询器

当前实现：

- `startPolling()`
- `fetchLatestCodesOnce()`
- `parseIntegratedMessage(...)`

### 6.6.1 `startPolling()`

职责：

- 单例启动
- 按配置间隔执行 `fetchLatestCodesOnce()`

### 6.6.2 `fetchLatestCodesOnce()`

职责：

- 连接 IMAP
- 选中 INBOX
- 只抓最近一部分邮件
- 遍历邮件并解析

### 6.6.3 `parseIntegratedMessage(...)`

职责：

- 只处理看起来像 OpenAI / ChatGPT 的邮件
- 从 subject / body 提取验证码
- 从邮件头里推断真正目标邮箱
- 构造成 `IntegratedIMAPCode`

这里的设计重点不是“能抓到验证码”，而是：

- 能把验证码正确归属到具体邮箱

这对 catch-all 域名邮箱模式至关重要。

---

## 7. Temp Mail 资源层

当前实现：`api-register-go/temp_mail.go`

这是移植时第二个最值得整体保留的模块。

---

## 7.1 这个模块解决的不是“收码”，而是“资源管理”

Temp Mail 模块的职责不只是抽验证码，它同时负责：

1. 准备临时邮箱客户端
2. 维护当前 mailbox session
3. 防止复用旧 mailbox
4. 处理创建邮箱频率限制
5. 在不同 provider 间切换
6. 从消息中抽验证码

所以它应该被视为：

- `MailboxProvider + OTPSource`

而不是一个单纯的 regex 工具。

---

## 7.2 它保存什么状态

`TempMailService` 里最重要的字段：

- `httpClient`
- `proxy`
- `createGap`
- `provider`
- `token`
- `currentMailbox`
- `firstServed`
- `freshOnFirst`
- `lastCreatedAt`
- `mailTMDomain`
- `detailCache`

这些字段你在移植时最好原样保留语义。

### 关键语义

- `currentMailbox`: 当前活动邮箱
- `provider`: 当前来自 `temp-mail.org` 还是 `mail.tm`
- `freshOnFirst`: 新任务首个账号也要强制 fresh mailbox
- `createGap`: 创建下一个 mailbox 前最少等待多久
- `detailCache`: mail.tm 的详情缓存，防止同一封信重复抓 detail

---

## 7.3 配置阶段

函数：`Configure(proxy, cfg)`

职责：

- 记录代理
- 根据 `NextDelaySeconds` 更新 `createGap`
- 如果代理改变，则重建 HTTP client
- 重置任务级状态标记

注意：

- 它不会直接创建 mailbox
- 它只是准备 runtime 配置

移植时，这个分层值得保留：配置和资源申请不要绑死在一个函数里。

---

## 7.4 资源准备阶段

函数：`EnsureReady()`

职责：

1. 确保 `httpClient` 存在
2. 如果已有有效 mailbox，就直接沿用，但标记首个账号需 fresh
3. 如果本地 session 文件存在，尝试恢复
4. 如果仍没有可用 mailbox，就主动创建一个

这里的设计重点是：

- 允许持久化 session
- 但新任务首个账号仍可强制 fresh mailbox

这正是为了解决“服务重启后又拿回旧邮箱”的问题。

---

## 7.5 申请邮箱阶段

函数：`AcquireMailbox()`

行为：

- 新任务的第一个账号，如果需要 fresh，就强制拿新邮箱
- 后续账号也强制拿新邮箱
- 如果拿到的还是旧邮箱，会被判定为失败

这保证了“账号 -> 邮箱”之间尽量是一对一，而不是重复复用。

如果你未来搬到别的项目，这个规则不要删，否则很容易又遇到：

- 已被注册过的邮箱被再次分配

---

## 7.6 创建邮箱阶段

函数：

- `createFreshMailboxLocked(...)`
- `createOrRotateMailboxLocked(...)`
- `createMailTMMailboxLocked(...)`

### 7.6.1 `createOrRotateMailboxLocked(...)`

这是 provider 层的主逻辑。

当前策略：

1. 如果当前 provider 已是 `mailtm`，直接走 `mail.tm`
2. 如果距离上次创建还没过 `createGap`，先等待
3. 调 `temp-mail.org /mailbox`
4. 如果 429，则指数式延长等待继续重试
5. 如果持续失败：
   - 若现有邮箱仍可用，可继续保留
   - 若错误像限流，则切到 `mail.tm`

这相当于一个“邮箱 provider 的局部故障转移器”。

### 7.6.2 `createFreshMailboxLocked(...)`

这个函数在 `createOrRotateMailboxLocked(...)` 外又加了一层保护：

- 如果新拿到的邮箱和上一个完全一样，则阻止复用
- 如果当前 provider 是 `temp-mail.org`，会自动尝试切到 `mail.tm`

这是当前 Temp Mail 稳定性的核心。

### 7.6.3 `createMailTMMailboxLocked(...)`

逻辑：

- 先抓可用 domain
- 随机生成 local part
- 创建 `mail.tm` 账号
- 获取 `mail.tm` token
- 保存为当前 provider 状态

这里的意义是：

- `mail.tm` 不是单纯备用域名
- 它是一个完整的备用邮箱服务

---

## 7.7 Temp Mail 抓信与抽码

关键函数：

- `fetchRowsLocked()`
- `fetchRowsMailTMLocked()`
- `FindCode(...)`
- `findBestTempMailCode(...)`
- `extractTempMailCode(...)`

### 7.7.1 `fetchRowsLocked()`

职责：

- 根据当前 provider 抓取消息列表
- 把 provider 返回格式统一映射成 `tempMailRow`

统一字段：

- `ID`
- `Received`
- `Text`

这样后面的抽码逻辑就不用关心 provider 差异。

### 7.7.2 `fetchRowsMailTMLocked()`

`mail.tm` 有一层特殊优化：

- 先抓消息列表
- 如果列表项看起来像 OpenAI / ChatGPT 候选，但列表内容还不够完整
- 再按 message id 拉 detail
- detail 会缓存，避免重复请求

这非常值得保留，因为很多临时邮箱接口的列表页信息都不完整。

### 7.7.3 `extractTempMailCode(...)`

当前抽码规则不是“任何 6 位数字都算”，而是：

1. 先删除邮箱地址，避免误把地址中的数字当 OTP
2. 必须包含 `chatgpt`
3. 优先抓 `chatgpt` 附近的 6 位数字
4. 找不到才回退更宽松的规则

这使得它对多语言内容依然稳定，因为真正依赖的是：

- 关键字候选
- 6 位数字距离

### 7.7.4 `findBestTempMailCode(...)`

这个函数的一个非常重要的细节是：

- 如果一条消息看起来是候选，但当前还没抽到 code，不会立刻标记为永久 seen

这样做是为了解决：

- 第一轮只有标题，没有正文
- 第二轮正文才补齐

移植时这一点必须保留，否则你会出现“明明邮件到了，但系统错过验证码”的问题。

---

## 7.8 Temp Mail OTP 等待链路

函数：`waitForTempMailCode(...)`

这个函数的设计很值得保留，因为它没有单独做另一套等待系统，而是：

1. 启动 resend goroutine
2. 启动 Temp Mail polling goroutine
3. polling 一旦抓到 code，就调用 `integratedIMAP.InjectManualCode(...)`
4. 最终仍通过 `WaitVerificationCode(...)` 统一等待返回

也就是说，Temp Mail 虽然是另一种来源，但最终还是接入统一 OTP 总线。

这是一个非常好的设计：

- 主流程只关心 `WaitVerificationCode`
- 各种来源自己往总线里塞 code

移植时建议完全保留这种模式。

---

## 8. 人工兜底桥接层

当前实现：`api-register-go/manual_code.go`

这个模块常被低估，但实际上它让系统从“全自动脚本”升级成“可运营系统”。

---

## 8.1 两类手动输入

它支持两种人工介入：

1. 手动输入验证码
2. 手动输入临时邮箱

### 8.1.1 手动验证码

支持：

- `123456`
- `email----123456`
- `email 123456`

如果当前只有一个 waiter，也允许直接输入 6 位数字。

### 8.1.2 手动临时邮箱

当 Temp Mail provider 限流，自动申请邮箱失败时：

- `waitManualMailboxInput(...)`

会把当前任务挂进一个邮箱等待队列。

用户只要在终端直接输入：

```text
xxx@xxx.com
```

就会被投递给当前等待者。

---

## 8.2 这个层的真正作用

它不是“方便调试”，而是解决自动化系统无法避免的最后 1%：

- 外部邮件服务异常
- 用户已经人工看到 OTP
- 自动创建临时邮箱失败

如果你要在别的项目里复用这套逻辑，建议保留“人工桥接层”，哪怕你把终端输入改成：

- 后台管理面板注入
- WebSocket 注入
- 命令总线注入

其抽象仍然应该是：

- 外部人工事件 -> 注入 OTP 总线 / 注入 Mailbox 队列

---

## 9. 自动重试与局部故障保护

---

## 9.1 账号级重试

当前 Go 版最外层有：

- `MaxRetry = 2`

也就是单账号失败后最多重试两次。

这层重试适合保留，因为它覆盖的是真正完整业务链，而不是某个局部请求。

---

## 9.2 OTP 自动重发

当前策略统一为：

- 首次 20 秒后重发
- 之后每 25 秒重发

适用场景：

- 普通 IMAP
- Outlook IMAP
- Temp Mail

如果你未来移植，可以把这个提取成一个统一 helper：

```text
startResendLoop(firstDelay=20s, interval=25s, resendFn)
```

这样能减少三套路径分别维护的复杂度。

---

## 9.3 IMAP 自动重连

Outlook 路径里：

- 连接失效时会重连

Python 版里还额外做了：

- 连续失败 2 次后强制重连

如果你未来要加强 Go 版，也可以把 Python 的这段策略移植过去。

---

## 9.4 失败率中止

只有 Python Web 版有显式滑动窗口失败率保护：

- 最近窗口大小 = `max(workers * 3, 10)`
- 至少 5 个结果后开始判断
- 失败率 >= 90% 时停止后续任务

Go 版当前没有这层全局熔断。

如果你要移植时增强系统稳定性，建议把这层加回 Go 版或你的新项目里。

---

## 10. 你移植时应该保留的关键约束

下面这些不是风格问题，而是逻辑正确性的关键点。

### 10.1 `otpSentAt` 必须在提交邮箱前记录

否则你会无法正确过滤“旧邮件”。

### 10.2 `create_account_password` 后必须走 `user/register`

不要误走旧的 `passwordless/send-otp`。

### 10.3 OTP 获取必须支持 `minTime`

否则二次注册时很容易复用到上一次验证码。

### 10.4 Temp Mail 候选消息未出码时不能立刻永久跳过

否则你会漏掉第二轮才补全文本的邮件。

### 10.5 Temp Mail 首个账号也要能强制 fresh mailbox

否则重启后可能直接复用旧邮箱。

### 10.6 OTP 总线必须支持 `InjectCode`

否则 Temp Mail 和人工输入会被迫走完全不同的等待逻辑，后期会很难维护。

### 10.7 主流程只负责“调用 OTP 等待”，不要自己实现具体抓码细节

否则注册状态机会和 OTP 来源耦合得很死。

---

## 11. 推荐的可移植模块接口

如果你准备正式重构到新项目，我建议按下面接口切。

### 11.1 Task Runner

```go
type TaskRunner interface {
    StartBatch(req StartRequest) error
    StopBatch()
}
```

### 11.2 Registration Engine

```go
type RegistrationEngine interface {
    Register(account Account, ctx RunContext) (*RegResult, error)
}
```

### 11.3 OTP Bus

```go
type OTPBus interface {
    WaitForCode(email string, timeout time.Duration, minTime time.Time) (string, error)
    InjectCode(email, code, source string) error
    ConsumeCode(email, code string) error
}
```

### 11.4 Mailbox Provider

```go
type MailboxProvider interface {
    Configure(proxy string, cfg TempMailConfig)
    EnsureReady() error
    AcquireMailbox() (string, error)
    FindCode(expectedEmail string, minTime time.Time, seen map[string]struct{}) (string, error)
}
```

### 11.5 Transport

```go
type RegistrationTransport interface {
    Get(url string) (int, string, error)
    PostJSON(url string, payload any, headers map[string]string) (int, string, error)
    PostForm(url string, data url.Values) (int, string, error)
    FollowRedirects(startURL string, maxHops int) (string, error)
    GetCookie(name string) string
}
```

只要你在新项目里先把这 5 组接口立住，当前逻辑就很容易分步搬过去。

---

## 12. 最小移植顺序

如果你不是要重写全部系统，而是先做可运行版本，建议按这个顺序搬：

### 第一阶段

- 搬 `HTTPClient`
- 搬 `registerAccount(...)`
- 固定只支持一种 OTP 来源

### 第二阶段

- 搬 `IntegratedIMAPService`
- 把 OTP 等待改成统一总线

### 第三阶段

- 搬 `TempMailService`
- 接到 OTP 总线

### 第四阶段

- 搬 `manual_code.go`
- 增加人工兜底

### 第五阶段

- 搬 worker、结果存储、跳过已完成、失败率中止

这样做的好处是：

- 先保证注册主链路可跑
- 再逐步补稳定性和运营能力

---

## 13. 当前项目里哪些部分不建议直接照搬

### 13.1 Dashboard 相关代码

UI 本身不是核心逻辑，可以按你自己的系统重写。

### 13.2 文件落盘格式

当前结果保存方式适合单机小规模任务，不一定适合服务化系统。

### 13.3 代理模型

当前只有单代理注入，没有完整代理池。

如果你新项目要做真正的动态代理转发：

- 需要另外设计代理池、健康检查和熔断状态机

不要误以为当前项目已经实现了这整套东西。

---

## 14. 一句话落地建议

如果你现在马上要把这套逻辑搬到别的项目，最稳的做法是：

1. 先按 `注册状态机 + OTP 总线 + Temp Mail 资源层` 三大块拆模块
2. 保留当前 Go 版的状态顺序和分支判断
3. 把 UI、日志、文件存储替换成你目标项目自己的方式
4. 不要先做代理池，先把单代理注入跑通

这样移植风险最低，也最不容易把已经验证过的注册链路搞坏。
