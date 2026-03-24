# openai-auto-register-main 逻辑抽取说明

本文只基于 `C:\Users\fi\source\openai-auto-register-main` 当前代码整理，目标是把你关心的几块逻辑抽出来讲清楚：

- 凭证池 / 账号池 / 阵列轮换
- 代理、重试、熔断
- OTP 验证、IMAP、Temp Mail

这份文档不会把别的项目里的实现硬套进来。换句话说，下面会明确区分：

- 这个 repo 里已经存在的逻辑
- 这个 repo 里没有独立实现、但有近似行为的逻辑

---

## 1. 目录与主实现位置

这个仓库里和你要求最相关的代码，主要分成三块：

### 1.1 当前主实现：`api-register-go`

这是当前最完整、功能最全的一套实现，包含：

- API 注册主流程
- Dashboard / SSE
- Temp Mail
- 集成 IMAP
- 终端手动输入 OTP / 手动输入邮箱
- worker 并发、批量任务、结果落盘

关键文件：

- `api-register-go/main.go`
- `api-register-go/temp_mail.go`
- `api-register-go/imap_merged.go`
- `api-register-go/manual_code.go`

### 1.2 旧版 API 实现：`api-register-py`

这是较早的一套 Python 实现，仍有参考价值，尤其是：

- 旧版 worker 组织方式
- 失败率滑动窗口中止
- IMAP 轮询与重连

关键文件：

- `api-register-py/api_register.py`
- `api-register-py/web_server.py`

### 1.3 浏览器流：`browser-register-py`

这一块是浏览器自动化注册流，不是当前你要抽取的 OTP / Temp Mail 主实现，也不构成这个文档的核心。

---

## 2. 先说结论：这个项目实际有没有你提到的那些模块

为了避免后续移植时认知偏差，先把事实说清楚。

### 2.1 有的

- 有账号输入队列
- 有 Temp Mail 占位账号批量生成
- 有 worker 并发调度
- 有按账号重试
- 有 OTP 自动重发
- 有 IMAP 轮询、去重、消费
- 有 Temp Mail 邮箱轮换
- 有 Temp Mail 服务限流后的备用服务切换
- 有终端手动补 OTP / 手动补邮箱

### 2.2 没有独立做成模块的

- 没有真正意义上的“凭证池服务”
- 没有真正意义上的“动态代理转发池”
- 没有多代理健康分级、自动切换上游、代理熔断恢复的完整代理网关
- 没有独立的“熔断中心”

### 2.3 最接近你描述的等价物

- “凭证池 / 阵列轮换”的等价物：
  - 账号列表解析
  - Temp Mail 占位账号批量生成
  - worker 调度
  - 每个任务邮箱动态分配
  - 成功后延迟切换到下一个账号

- “动态代理转发 / 熔断 / 自动重试”的等价物：
  - 单代理注入到 HTTP Client / Session
  - Temp Mail 服务限流后切换到 `mail.tm`
  - 按账号重试
  - IMAP 错误自动重连
  - Python 版滑动窗口失败率中止

---

## 3. 凭证池 / 账号池 / 阵列轮换

严格说，这个项目没有一个独立的“凭证池服务”，但已经具备“账号池 + 任务队列 + 轮换执行”的完整雏形。

---

## 3.1 账号输入来源

位置：`api-register-go/main.go`

主入口：

- `type Account`
- `type StartRequest`
- `handleStart(...)`

### 3.1.1 普通账号模式

Dashboard 提交的 `accounts` 文本会被按行解析，每行格式是：

```text
email----password----client_id----refresh_token
```

其中：

- 前两段是基础账号凭证
- 后两段可选，主要给 Outlook OAuth / Refresh Token 路径使用

这部分相当于最基础的“外部凭证池输入”。

### 3.1.2 Temp Mail 模式

如果请求里包含 `temp_mail.count`，则不会使用真实邮箱列表，而是先生成一批占位账号：

```text
temp-mail-1@placeholder.local
temp-mail-2@placeholder.local
...
```

这些占位账号不会真正参与收信，它们只是任务槽位。真正的邮箱会在执行时由 `TempMailService.AcquireMailbox()` 动态分配。

这部分是这个项目里最接近“阵列轮换”的设计：

- 前端只给出数量
- 后端在运行时为每个槽位动态分配真正邮箱
- 成功后切到下一个槽位

### 3.1.3 自动识别占位账号

如果用户输入的账号列表全是 `@placeholder.local`，Go 版本会自动切换成 Temp Mail 模式，而不是按普通账号处理。

也就是说，这个项目在“账号池”和“临时邮箱任务池”之间已经做了统一入口。

---

## 3.2 任务过滤与结果去重

位置：`api-register-go/main.go`

相关逻辑：

- `getFinishedEmails()`
- `handleStart(...)` 中的 `SkipFinished`
- 结果落盘到 `tokens/` 目录

行为：

- 每个成功结果都会按邮箱落盘成 json
- 启动任务时如果开启 `SkipFinished`
- 已存在结果文件的邮箱会被过滤掉，不再重复跑

这相当于一个非常轻量的“已消费凭证集合”。

它不是数据库式的凭证池，但已经具备：

- 去重
- 恢复跑
- 跳过已完成账号

---

## 3.3 Go 版 worker 阵列调度

位置：`api-register-go/main.go`

关键函数：

- `normalizeTempWorkers(...)`
- `runWorkers(...)`
- `doOne(...)`

### 3.3.1 调度模型

Go 版不是线程池类，而是：

- `sem := make(chan struct{}, workers)` 作为并发信号量
- 对每个账号起 goroutine
- 同时最多运行 `workers` 个

这是一种“数组式槽位调度”，本质上就是：

- 一批账号排队
- 按最大并发占用固定数量槽位
- 槽位空出来后继续跑下一个

### 3.3.2 Temp Mail 的 worker 约束

Go 版专门做了 `normalizeTempWorkers(...)`：

- `AllowParallel = false` 时，强制并发为 `1`
- `AllowParallel = true` 时，允许使用前端给的并发数
- 会限制到合理范围，防止无限并发

这是 Temp Mail 模式的核心保护逻辑，因为短时间创建大量 mailbox 很容易触发上游限流。

### 3.3.3 单账号执行单元

`doOne(...)` 是真正的“阵列元素执行器”。

一个账号槽位的生命周期如下：

1. 确定显示名称
2. 如果是 Temp Mail 模式，先动态分配邮箱
3. 分配失败时，允许终端手动输入邮箱
4. 进入最多 `MaxRetry = 2` 次的注册重试
5. 成功后落盘 token 结果
6. 如果是 Temp Mail 且设置了延迟，则等待一段时间再切下一个

这部分可以直接抽成一个通用的“任务执行器 + 资源分配器”。

---

## 3.4 Temp Mail 的邮箱轮换

位置：`api-register-go/temp_mail.go`

关键函数：

- `Configure(...)`
- `EnsureReady(...)`
- `AcquireMailbox(...)`
- `createFreshMailboxLocked(...)`
- `createOrRotateMailboxLocked(...)`

### 3.4.1 设计目标

Temp Mail 逻辑的目标不是“固定一个邮箱收很多码”，而是：

- 每个账号尽量拿到新的邮箱
- 避免复用上一个邮箱
- 避免服务重启后拿回旧 session 里的同一个邮箱

### 3.4.2 当前轮换策略

`AcquireMailbox()` 的行为：

- 新任务首个账号，优先强制 fresh mailbox
- 后续账号，也强制 fresh mailbox
- 如果 `temp-mail.org` 返回的还是上一个邮箱，则阻止复用
- 如果仍无法拿到新邮箱，会自动切到 `mail.tm`

这部分本质上就是“邮箱资源池 + 防复用轮换”。

### 3.4.3 冷却与切换节奏

`MailboxCreateGap()` 直接绑定 `NextDelaySeconds`

也就是说，Dashboard 上“切换下一个前延迟（秒）”不只是控制成功后延迟，还会影响：

- 创建新 mailbox 前的最小间隔

这样做的目的，是让“跑下一个账号”和“创建下一封临时邮箱”的速率统一，减少触发上游限流的概率。

---

## 4. 代理、重试、熔断

这一块要分开看：这个项目里“代理”和“熔断”都存在近似实现，但没有独立的代理网关系统。

---

## 4.1 代理：当前是单代理注入，不是代理池

### 4.1.1 Go 版

位置：`api-register-go/main.go`、`api-register-go/temp_mail.go`

行为：

- `handleStart(...)` 从请求里拿 `proxy`
- `registerAccount(...)` 里构建 `NewHTTPClient(proxy)`
- `configureTempMailRuntime(proxy, cfg)` 把同一个代理传给 Temp Mail Runtime
- 如果代理发生变化，`TempMailService.Configure(...)` 会重建 HTTP client

这说明当前代理模型是：

- 整个任务使用一个代理
- 主注册流程与 Temp Mail 流程共享这个代理配置

它不是：

- 多代理池随机轮换
- 按请求动态选路
- 失败自动切换其他上游代理

### 4.1.2 Python 版

位置：`api-register-py/api_register.py`

行为：

- `APISession(proxy)` 内把 `proxy` 同时注入 `http` 和 `https`
- 同一个账号执行期间都使用同一代理

这仍然是单代理注入。

---

## 4.2 自动重试：这个项目是有完整实现的

### 4.2.1 账号级重试

Go 版：

- `main.go`
- `MaxRetry = 2`
- `doOne(...)` 内对 `registerAccount(...)` 做最多 2 次重试

Python 版：

- `api_register.py`
- `MAX_RETRY_PER_ACCOUNT = 2`
- `_do_one(...)` 内对 `register_account(...)` 做最多 2 次重试

这是最核心的“任务级自动重试”。

### 4.2.2 OTP 重发

Go 版：

- `waitForCode(...)`
- 首次 20 秒后自动 resend
- 之后每 25 秒 resend 一次

Outlook IMAP 路径：

- `fetchOutlookCode(...)`
- 同样有 20 秒后首次 resend、之后 25 秒间隔 resend

Temp Mail 路径：

- `waitForTempMailCode(...)`
- 内部也复用 resend 逻辑

Python 版：

- `poll_verification_code(...)`
- 同样是 20 秒后开始重发，之后按固定间隔重发

### 4.2.3 IMAP 自动重连

Python 版：

- `poll_verification_code(...)`
- 连续失败两次后触发 IMAP 重连

Go Outlook 路径：

- `fetchOutlookCode(...)`
- `Select` 失败或连接失效时会重新建立连接

### 4.2.4 Temp Mail mailbox 创建重试

位置：`api-register-go/temp_mail.go`

函数：

- `createOrRotateMailboxLocked(...)`

逻辑：

- 请求创建 mailbox 失败时自动重试
- 429 时采用递增等待
- 等待超过窗口后决定 fallback

这块实际上已经接近“上游服务请求自动重试器”。

---

## 4.3 熔断：这个项目只有局部近似，不是完整熔断系统

### 4.3.1 最接近熔断的实现：Python Web 版失败率中止

位置：`api-register-py/web_server.py`

函数：

- `_register_worker(...)`
- `_should_abort()`

逻辑：

- 用滑动窗口记录最近结果
- 窗口大小为 `max(workers * 3, 10)`
- 至少有 5 个结果后才开始判断
- 如果最近窗口失败率 >= 90%
- 则设置 `abort_flag = True`
- 停止后续任务并广播错误

这部分是这个 repo 里最接近“熔断”的机制。

它的性质是：

- 针对整批任务的失败率保护
- 不是代理熔断
- 不是单上游服务健康评分

### 4.3.2 Go Temp Mail 的局部保护

位置：`api-register-go/temp_mail.go`

`createOrRotateMailboxLocked(...)` 在 `temp-mail.org` 连续限流时会：

- 递增重试
- 超时后 fallback 到 `mail.tm`
- 如果已有可用邮箱，则可能继续复用当前邮箱

这更像“上游服务故障转移”，而不是正式熔断器。

### 4.3.3 当前没有的部分

这个仓库当前没有：

- 代理健康分数
- 代理半开恢复
- 代理黑名单 TTL
- 多上游代理轮询
- 请求级 fallback routing

如果你要把“动态代理转发熔断”抽成独立模块，需要额外设计，不是直接从现有代码整块复制就有。

---

## 5. OTP 验证与邮件获取主链路

这一块是这个项目最成熟的一部分。

---

## 5.1 Go 主注册流程中的 OTP 分支

位置：`api-register-go/main.go`

关键函数：

- `registerAccount(...)`

当前 Go 版的核心流程大致如下：

1. 发起 OAuth
2. 获取 Sentinel token
3. 提交邮箱
4. 根据页面类型判断分支
5. 如果是 `create_account_password`
   - 走 `POST /api/accounts/user/register`
6. 如果下一页是 `email_otp_send` / `email_otp_verification`
   - 开始 OTP 流程
7. 通过 IMAP / Temp Mail / 手动输入拿验证码
8. `email-otp/validate`
9. 创建账号
10. 选择 workspace
11. 交换 token

这里最重要的一点是：

- 新版 Go 已不再把 `create_account_password` 错误地接到 `passwordless/send-otp`
- 而是先走 `user/register`

这也是这个项目最近修过的重要逻辑。

---

## 5.2 集成 IMAP：统一 OTP 中枢

位置：`api-register-go/imap_merged.go`

关键对象：

- `IntegratedIMAPService`

关键函数：

- `ConfigureIntegratedIMAP(...)`
- `AutoLoadConfig()`
- `WaitForCode(...)`
- `ConsumeCode(...)`
- `startPolling()`
- `fetchLatestCodesOnce()`
- `upsertCode(...)`
- `saveCodes()`

### 5.2.1 它做的事

这个服务实际上承担了“OTP 中心”的职责：

- 自动加载 IMAP 配置
- 周期性轮询邮箱
- 抓取验证码
- 存进本地 `codes.json`
- 为正在等待某邮箱验证码的调用方提供 waiter channel
- 支持消费与去重

### 5.2.2 为什么它重要

`waitForCode(...)` 并不是每次都自己连 IMAP；它优先等这个集成服务交付验证码。

因此这个服务已经具备一种“消息总线 / 验证码缓存中心”的性质。

---

## 5.3 普通邮箱 OTP 等待

位置：`api-register-go/main.go`

函数：

- `waitForCode(...)`

逻辑：

- 先清理旧验证码，避免复用历史 code
- 以 `otpSentAt - 60s` 作为最早接受时间
- 启动后台 resend goroutine
- 给终端打印手动输入提示
- 调用 `WaitVerificationCode(email, PollTimeout, minTime)`
- 一旦拿到 code，就返回给注册主流程

这是一条“统一等待链路”：

- 集成 IMAP 能投递
- Temp Mail 也能往里注入
- 手动输入也能往里注入

也就是说，这里虽然表面是 `waitForCode(...)`，实际上背后已经统一了多个 OTP 来源。

---

## 5.4 Outlook 专用 IMAP 路径

位置：`api-register-go/main.go`

函数：

- `fetchOutlookCode(...)`

逻辑特点：

- 优先 XOAUTH2
- 失败后回退账号密码登录
- 拉最近邮件
- 按时间窗口过滤旧邮件
- 定时 resend
- 连接失效时自动 reconnect

这是单账号独立 IMAP 路径，不走统一的 catch-all Hub。

---

## 5.5 Temp Mail OTP 链路

位置：`api-register-go/temp_mail.go`

关键函数：

- `waitForTempMailCode(...)`
- `FindCode(...)`
- `findBestTempMailCode(...)`
- `extractTempMailCode(...)`
- `isTempMailCodeCandidate(...)`

### 5.5.1 流程

Temp Mail OTP 路径的结构是：

1. 准备好当前 mailbox
2. 发送 OTP
3. 启动后台轮询 Temp Mail 消息
4. 查找包含 OpenAI / ChatGPT 特征的候选消息
5. 从候选消息里抽取 6 位验证码
6. 把验证码注入到统一等待链路
7. 主注册流程继续完成后续步骤

### 5.5.2 抽码策略

当前实现不是“随便抓 6 位数字”，而是：

- 先排除邮件地址，避免把邮箱名中的数字误识别成 OTP
- 判断是否是 ChatGPT / OpenAI 候选内容
- 优先抓与 ChatGPT 语义距离近的 6 位数字
- 候选邮件如果第一次还没出现验证码，不会立刻永久跳过

这部分是为了解决 Temp Mail 页面里：

- 广告信
- 多语言邮件
- 邮件内容分段加载
- 首轮只有标题、次轮才补全文本

### 5.5.3 provider fallback

如果 `temp-mail.org` 创建邮箱持续限流：

- 会自动切到 `mail.tm`

这条链路是整个 Temp Mail 模块稳定性的核心。

---

## 5.6 终端手动输入 OTP / 手动输入邮箱

位置：`api-register-go/manual_code.go`

关键函数：

- `startManualCodeInput()`
- `parseManualCodeInput(...)`
- `manualCodeHint(...)`
- `waitManualMailboxInput(...)`
- `InjectManualCode(...)`

### 5.6.1 支持的输入形态

终端里可以直接输入：

- `123456`
- `email----123456`
- 手动输入完整临时邮箱地址

### 5.6.2 作用

它解决了两个兜底场景：

- OTP 自动抓取失败，但人已经看到验证码
- Temp Mail 服务拿邮箱失败，但人手动准备了邮箱

这一层非常适合被抽成“人工介入桥接层”。

---

## 6. Python 版里值得一起抽出来的逻辑

虽然 Go 版现在是主实现，但 Python 版有两块仍然有参考价值。

---

## 6.1 滑动窗口失败率中止

位置：`api-register-py/web_server.py`

价值：

- 对整批任务进行自保护
- 当近期失败率过高时停止继续放大损失

这适合在你未来要做“真正熔断层”时复用为批任务保护器。

---

## 6.2 轮询节奏渐进放缓

位置：`api-register-py/api_register.py`

函数：

- `poll_verification_code(...)`

特点：

- 轮询间隔是 `[3, 4, 5, 6, 8, 10]`
- 前期更积极
- 后期放缓，降低 IMAP 压力

这对未来把 OTP polling 做成独立组件时有参考意义。

---

## 7. 可以怎么“整块抽”成独立模块

如果你的目标是把这套逻辑搬去别的项目，建议按下面边界切：

### 7.1 账号任务层

推荐抽取内容：

- `handleStart(...)` 里的账号解析思路
- placeholder temp-mail 槽位生成
- `SkipFinished` 过滤
- `runWorkers(...)`
- `doOne(...)`

模块职责：

- 接任务
- 排队
- 控并发
- 重试
- 写结果

### 7.2 OTP 中枢层

推荐抽取内容：

- `IntegratedIMAPService`
- `waitForCode(...)`
- `InjectManualCode(...)`

模块职责：

- 统一 OTP 等待接口
- 接入多个 code 来源
- 去重 / 消费 / 过期控制

### 7.3 Temp Mail 资源层

推荐抽取内容：

- `TempMailService`
- `AcquireMailbox(...)`
- `createFreshMailboxLocked(...)`
- `createOrRotateMailboxLocked(...)`
- `FindCode(...)`

模块职责：

- 申请邮箱
- provider fallback
- 限流等待
- 邮件解析
- OTP 抽取

### 7.4 代理与失败保护层

当前能抽的只有：

- 单代理注入
- 按账号重试
- Python 滑动窗口失败率中止

如果你要“真正的动态代理转发熔断”，需要补设计：

- 代理池
- 代理健康分
- 熔断状态机
- 半开恢复
- 每请求路由策略

---

## 8. 最终判断：这个项目最值得复用的不是哪一段

如果只挑最有价值、最成熟的部分，这个仓库最值得直接复用的是：

1. Go 版 `registerAccount(...)` 主流程与 page type 分支
2. `IntegratedIMAPService` 统一 OTP 中枢
3. `TempMailService` 的邮箱分配、限流等待、provider fallback、验证码抽取
4. `manual_code.go` 的人工兜底桥接
5. `runWorkers(...) + doOne(...)` 的批量任务执行框架

真正不够成熟、不能直接当成“整块代理系统”拿走的，是：

- 动态代理转发
- 代理熔断
- 代理池健康调度

这个 repo 里只有雏形，没有完整实现。

---

## 9. 关键代码索引

### Go 主实现

- `api-register-go/main.go`
  - `registerAccount(...)`
  - `waitForCode(...)`
  - `fetchOutlookCode(...)`
  - `normalizeTempWorkers(...)`
  - `handleStart(...)`
  - `runWorkers(...)`
  - `doOne(...)`
  - `sleepFlow(...)`

- `api-register-go/temp_mail.go`
  - `TempMailConfig`
  - `Configure(...)`
  - `EnsureReady(...)`
  - `AcquireMailbox(...)`
  - `createFreshMailboxLocked(...)`
  - `createOrRotateMailboxLocked(...)`
  - `FindCode(...)`
  - `waitForTempMailCode(...)`
  - `extractTempMailCode(...)`
  - `isTempMailCodeCandidate(...)`
  - `findBestTempMailCode(...)`

- `api-register-go/imap_merged.go`
  - `IntegratedIMAPService`
  - `ConfigureIntegratedIMAP(...)`
  - `AutoLoadConfig()`
  - `WaitForCode(...)`
  - `ConsumeCode(...)`
  - `startPolling()`
  - `fetchLatestCodesOnce()`
  - `upsertCode(...)`
  - `saveCodes()`

- `api-register-go/manual_code.go`
  - `startManualCodeInput()`
  - `parseManualCodeInput(...)`
  - `manualCodeHint(...)`
  - `waitManualMailboxInput(...)`
  - `InjectManualCode(...)`

### Python 参考实现

- `api-register-py/api_register.py`
  - `poll_verification_code(...)`
  - `APISession`
  - `register_account(...)`
  - `_do_one(...)`

- `api-register-py/web_server.py`
  - `_register_worker(...)`
  - `_should_abort()`

---

## 10. 一句话总结

`openai-auto-register-main` 真正能整块抽走的核心，不是“完整代理熔断系统”，而是：

- 账号任务调度
- OTP 中枢
- Temp Mail 资源与验证码链路
- 人工兜底输入桥接

而“动态代理转发熔断”在这个项目里目前只有局部近似实现，还需要你自己再补一层架构。
