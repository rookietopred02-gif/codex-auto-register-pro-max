package main

import (
	"fmt"
	"strings"
	"sync/atomic"
)

const (
	langEN   = "en"
	langZHTW = "zh-TW"
)

type textReplacement struct {
	old string
	new string
}

var activeUILanguage atomic.Value

func init() {
	activeUILanguage.Store(langEN)
}

func normalizeLanguage(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "zh-tw", "zh_tw", "zh-hant", "zh-hant-tw", "tw", "traditional":
		return langZHTW
	default:
		return langEN
	}
}

func setActiveLanguage(raw string) {
	activeUILanguage.Store(normalizeLanguage(raw))
}

func activeLanguage() string {
	if v, ok := activeUILanguage.Load().(string); ok {
		return normalizeLanguage(v)
	}
	return langEN
}

func localizeRuntimeText(lang, text string) string {
	switch normalizeLanguage(lang) {
	case langZHTW:
		return applyTextReplacements(text, runtimeReplacementsZHTW)
	default:
		return applyTextReplacements(text, runtimeReplacementsEN)
	}
}

func applyTextReplacements(text string, replacements []textReplacement) string {
	out := text
	for _, item := range replacements {
		out = strings.ReplaceAll(out, item.old, item.new)
	}
	return out
}

func localizePayload(v interface{}) interface{} {
	switch payload := v.(type) {
	case map[string]interface{}:
		cloned := make(map[string]interface{}, len(payload))
		for key, value := range payload {
			if text, ok := value.(string); ok && (key == "error" || key == "text" || key == "message") {
				cloned[key] = localizeRuntimeText(activeLanguage(), text)
				continue
			}
			cloned[key] = value
		}
		return cloned
	default:
		return v
	}
}

var runtimeReplacementsEN = []textReplacement{
	{old: "本地数据库中没有可用的临时邮箱域名", new: "No available temp-mail domains remain in the local database"},
	{old: "临时邮箱域名已在本地数据库中标记为不可用", new: "Temp-mail domain is marked unavailable in the local database"},
	{old: "跳过数据库中已标记不可用的临时邮箱域名", new: "Skipping temp-mail domain already marked unavailable in the database"},
	{old: "该域名已在本地数据库中标记为不可用", new: "This domain is already marked unavailable in the local database"},
	{old: "持久化临时邮箱域名失败", new: "Failed to persist temp-mail domain"},
	{old: "当前尚未生成 workspace，需要先完成该流程", new: "workspace has not been created yet; finish that flow first"},
	{old: "当前尚未生成 workspace", new: "workspace has not been created yet"},
	{old: "新账号已创建，重启登录流程以获取 workspace/token...", new: "Account created, restarting login flow to fetch workspace/token..."},
	{old: "重新登录已进入邮箱验证码阶段", new: "Re-login reached the email OTP step"},
	{old: "等待重新登录验证码", new: "Waiting for re-login OTP"},
	{old: "验证重新登录 OTP", new: "Verifying re-login OTP"},
	{old: "重新登录获取验证码失败", new: "Failed to retrieve re-login OTP"},
	{old: "重新登录 OTP 验证失败", new: "Re-login OTP verification failed"},
	{old: "重新登录未进入验证码页面", new: "Re-login did not reach the OTP page"},
	{old: "重新登录未进入密码页面", new: "Re-login did not reach the password page"},
	{old: "重新登录提交邮箱失败", new: "Failed to submit re-login email"},
	{old: "重新登录提交密码失败", new: "Failed to submit re-login password"},
	{old: "访问重新登录密码页失败", new: "Failed to open the re-login password page"},
	{old: "访问重新登录验证码页失败", new: "Failed to open the re-login OTP page"},
	{old: "跟随重定向获取 Token...", new: "Following redirect to obtain token..."},
	{old: "选择 Workspace", new: "Selecting workspace"},
	{old: "选择 workspace 失败", new: "Workspace selection failed"},
	{old: "未找到 workspace", new: "Workspace not found"},
	{old: "账户创建后进入 ", new: "Post-create flow entered "},
	{old: "账户创建后页面", new: "Post-create page"},
	{old: "账户创建后续页面状态", new: "Post-create page status"},
	{old: "访问账户创建后续页面失败", new: "Failed to fetch post-create page"},
	{old: "创建账号失败", new: "Create account failed"},
	{old: "创建账号", new: "Create account"},
	{old: "跳过（账号已存在）", new: "Skipping (account already exists)"},
	{old: "验证 OTP", new: "Verifying OTP"},
	{old: "等待验证码", new: "Waiting for OTP"},
	{old: "跳过发送 OTP（服务器已自动发送）", new: "Skipping OTP send (already sent by the server)"},
	{old: "发送 OTP 失败", new: "Failed to send OTP"},
	{old: "发送 OTP...", new: "Sending OTP..."},
	{old: "验证码已发送到", new: "OTP sent to"},
	{old: "提交注册密码失败", new: "Failed to submit registration password"},
	{old: "提交注册密码...", new: "Submitting registration password..."},
	{old: "提交登录密码失败", new: "Failed to submit login password"},
	{old: "提交登录密码...", new: "Submitting login password..."},
	{old: "提交登录入口", new: "Submit login entry"},
	{old: "提交邮箱", new: "Submit email"},
	{old: "页面类型", new: "Page type"},
	{old: "下一页面", new: "Next page"},
	{old: "发起 OAuth", new: "Starting OAuth"},
	{old: "获取 Sentinel token...", new: "Fetching Sentinel token..."},
	{old: "检查 IP 地理位置失败", new: "IP geolocation check failed"},
	{old: "检查 IP 地理位置...", new: "Checking IP geolocation..."},
	{old: "IP 地理位置不支持", new: "IP geolocation unsupported"},
	{old: "IP 位置", new: "IP location"},
	{old: "浏览器指纹", new: "Browser fingerprint"},
	{old: "Temp Mail 获取邮箱失败", new: "Temp Mail mailbox acquisition failed"},
	{old: "Temp Mail 初始化失败", new: "Temp Mail initialization failed"},
	{old: "Temp Mail 分配邮箱", new: "Temp Mail mailbox assigned"},
	{old: "Temp Mail 下一次重试将自动更换新邮箱，避免复用已失效地址", new: "Temp Mail will switch to a fresh mailbox on retry to avoid reusing an invalid address"},
	{old: "Temp Mail 模式已启用", new: "Temp Mail mode enabled"},
	{old: "Temp Mail 会限制短时间创建新邮箱，建议先用 1 个账号验证链路", new: "Temp Mail may rate-limit new mailbox creation; validate the flow with 1 account first"},
	{old: "Temp Mail 密码已自动升级为兼容默认值", new: "Temp Mail password was auto-upgraded to a compatible default"},
	{old: "Temp Mail 轮询异常", new: "Temp Mail polling warning"},
	{old: "Temp Mail 注入验证码失败", new: "Failed to inject Temp Mail OTP"},
	{old: "自动识别 Temp Mail 占位账号", new: "Auto-detected Temp Mail placeholder accounts"},
	{old: "占位账号密码已自动升级为兼容默认值", new: "Placeholder account password was auto-upgraded to a compatible default"},
	{old: "并发过高更容易触发限流，建议控制在 2-5", new: "High parallelism increases rate-limit risk; 2-5 workers is safer"},
	{old: "切换延迟", new: "next-account delay"},
	{old: "平行开关", new: "parallel"},
	{old: "固定并发 1", new: "fixed concurrency 1"},
	{old: "已获取 Token，", new: "Token acquired, "},
	{old: "秒后切换到下一个账号...", new: " seconds before switching to the next account..."},
	{old: "分配邮箱失败", new: "Mailbox allocation failed"},
	{old: "使用手动输入临时邮箱", new: "Using manually entered temp mailbox"},
	{old: "重试 #", new: "Retry #"},
	{old: "尝试 ", new: "Attempt "},
	{old: "  ❌ 尝试 ", new: "  ❌ Attempt "},
	{old: "  🚫 临时邮箱域名已写入本地数据库并标记为不可用", new: "  🚫 Temp-mail domain saved to the local database as unavailable"},
	{old: "注册成功", new: "Registration succeeded"},
	{old: "成功！", new: " succeeded!"},
	{old: "没有有效的账号", new: "No valid accounts"},
	{old: "所有账号已注册完毕", new: "All accounts are already processed"},
	{old: "已有任务运行中", new: "A task is already running"},
	{old: "OpenAI 注册密码至少需要 12 位，请在 Dashboard 调整密码后重试", new: "OpenAI registration password must be at least 12 characters. Update it in the dashboard and retry."},
	{old: "集成 IMAP 配置失败", new: "Failed to configure integrated IMAP"},
	{old: "集成 IMAP 已配置", new: "Integrated IMAP configured"},
	{old: "集成 IMAP", new: "Integrated IMAP"},
	{old: "验证码", new: "OTP"},
	{old: "邮箱", new: "email"},
	{old: "账号", new: "account"},
	{old: "并发", new: "workers"},
	{old: "注册转登录", new: "register→login"},
	{old: "重新登录", new: "re-login"},
	{old: "注册", new: "register"},
	{old: "登录", new: "login"},
	{old: "失败", new: "failed"},
	{old: "成功", new: "success"},
	{old: "已取消", new: "Cancelled"},
}

var runtimeReplacementsZHTW = []textReplacement{
	{old: "本地数据库中没有可用的临时邮箱域名", new: "本地資料庫中沒有可用的臨時郵箱域名"},
	{old: "临时邮箱域名已在本地数据库中标记为不可用", new: "臨時郵箱域名已在本地資料庫中標記為不可用"},
	{old: "跳过数据库中已标记不可用的临时邮箱域名", new: "跳過資料庫中已標記不可用的臨時郵箱域名"},
	{old: "该域名已在本地数据库中标记为不可用", new: "該域名已在本地資料庫中標記為不可用"},
	{old: "持久化临时邮箱域名失败", new: "持久化臨時郵箱域名失敗"},
	{old: "当前尚未生成 workspace，需要先完成该流程", new: "目前尚未生成 workspace，需要先完成該流程"},
	{old: "当前尚未生成 workspace", new: "目前尚未生成 workspace"},
	{old: "新账号已创建，重启登录流程以获取 workspace/token...", new: "新帳號已建立，重新啟動登入流程以取得 workspace/token..."},
	{old: "重新登录", new: "重新登入"},
	{old: "创建账号", new: "建立帳號"},
	{old: "账户创建后页面", new: "帳號建立後頁面"},
	{old: "账户创建后续页面状态", new: "帳號建立後續頁面狀態"},
	{old: "访问账户创建后续页面失败", new: "存取帳號建立後續頁面失敗"},
	{old: "账户创建后进入 ", new: "帳號建立後進入 "},
	{old: "等待重新登录验证码", new: "等待重新登入驗證碼"},
	{old: "验证重新登录 OTP", new: "驗證重新登入 OTP"},
	{old: "重新登录获取验证码失败", new: "重新登入取得驗證碼失敗"},
	{old: "重新登录 OTP 验证失败", new: "重新登入 OTP 驗證失敗"},
	{old: "重新登录未进入验证码页面", new: "重新登入未進入驗證碼頁面"},
	{old: "重新登录未进入密码页面", new: "重新登入未進入密碼頁面"},
	{old: "重新登录提交邮箱失败", new: "重新登入提交郵箱失敗"},
	{old: "重新登录提交密码失败", new: "重新登入提交密碼失敗"},
	{old: "访问重新登录密码页失败", new: "存取重新登入密碼頁失敗"},
	{old: "访问重新登录验证码页失败", new: "存取重新登入驗證碼頁失敗"},
	{old: "发起 OAuth", new: "發起 OAuth"},
	{old: "获取 Sentinel token...", new: "取得 Sentinel token..."},
	{old: "检查 IP 地理位置失败", new: "檢查 IP 地理位置失敗"},
	{old: "检查 IP 地理位置...", new: "檢查 IP 地理位置..."},
	{old: "IP 地理位置不支持", new: "IP 地理位置不支援"},
	{old: "IP 位置", new: "IP 位置"},
	{old: "浏览器指纹", new: "瀏覽器指紋"},
	{old: "页面类型", new: "頁面類型"},
	{old: "下一页面", new: "下一頁面"},
	{old: "验证码", new: "驗證碼"},
	{old: "邮箱", new: "郵箱"},
	{old: "账号", new: "帳號"},
	{old: "登录", new: "登入"},
	{old: "注册", new: "註冊"},
	{old: "失败", new: "失敗"},
	{old: "总耗时", new: "總耗時"},
	{old: "并发", new: "並發"},
	{old: "切换", new: "切換"},
	{old: "当前任务", new: "目前任務"},
	{old: "标记", new: "標記"},
	{old: "数据库", new: "資料庫"},
	{old: "临时邮箱", new: "臨時郵箱"},
	{old: "访问", new: "存取"},
	{old: "创建", new: "建立"},
	{old: "获取", new: "取得"},
	{old: "后续", new: "後續"},
	{old: "页", new: "頁"},
	{old: "状态", new: "狀態"},
	{old: "密码", new: "密碼"},
	{old: "载入", new: "載入"},
	{old: "已取消", new: "已取消"},
	{old: "当前", new: "目前"},
	{old: "暂无", new: "暫無"},
}

func localizedSSEError() string {
	return localizeRuntimeText(activeLanguage(), fmt.Sprintf("SSE not supported"))
}
