package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
)

const (
	tempmailLOLAPIBase   = "https://api.tempmail.lol/v2"
	tempMailPollInterval = 2 * time.Second
	tempMailDefaultGap   = 15 * time.Second
)

var (
	tempMailCodeRe        = regexp.MustCompile(`\b(\d{6})\b`)
	tempMailChatGPTCodeRe = regexp.MustCompile(`(?is)chatgpt[^A-Za-z0-9]{0,120}(\d{6})`)
	tempMailAfterCodeRe   = regexp.MustCompile(`(?is)[^A-Za-z0-9](\d{6})\b`)
	tempMailEmailRe       = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
)

type TempMailConfig struct {
	Count            int    `json:"count"`
	Password         string `json:"password"`
	AllowParallel    bool   `json:"allow_parallel,omitempty"`
	NextDelaySeconds *int   `json:"next_delay_seconds,omitempty"`
	APIBaseURL       string `json:"api_base_url,omitempty"`
}

func (c *TempMailConfig) PostSuccessDelaySeconds() int {
	if c == nil || c.NextDelaySeconds == nil {
		return 15
	}
	delay := *c.NextDelaySeconds
	if delay < 0 {
		return 15
	}
	if delay > 300 {
		return 300
	}
	return delay
}

func (c *TempMailConfig) MailboxCreateGap() time.Duration {
	return time.Duration(c.PostSuccessDelaySeconds()) * time.Second
}

func (c *TempMailConfig) TempmailLOLAPIBase() string {
	if c == nil {
		return tempmailLOLAPIBase
	}
	base := strings.TrimSpace(c.APIBaseURL)
	if base == "" {
		return tempmailLOLAPIBase
	}
	return strings.TrimRight(base, "/")
}

type tempMailRow struct {
	ID       string
	Received string
	Text     string
}

type tempmailLOLMailboxResp struct {
	Address string `json:"address"`
	Token   string `json:"token"`
}

type TempMailService struct {
	mu             sync.Mutex
	httpClient     *HTTPClient
	proxy          string
	createGap      time.Duration
	provider       string
	token          string
	currentMailbox string
	firstServed    bool
	freshOnFirst   bool
	lastCreatedAt  time.Time
	detailCache    map[string]string
	blockedDomains map[string]string
	apiBaseURL     string
}

var tempMailService = &TempMailService{createGap: tempMailDefaultGap}

type tempMailSession struct {
	Provider  string `json:"provider,omitempty"`
	Token     string `json:"token"`
	Mailbox   string `json:"mailbox"`
	UpdatedAt string `json:"updated_at"`
}

func ensureTempMailReady() error {
	return tempMailService.EnsureReady()
}

func acquireTempMailbox() (string, error) {
	return tempMailService.AcquireMailbox()
}

func rejectTempMailMailbox(mailbox, reason string) string {
	return tempMailService.MarkRejectedMailbox(mailbox, reason)
}

func configureTempMailRuntime(proxy string, cfg *TempMailConfig) {
	tempMailService.Configure(proxy, cfg)
}

func mailboxDomain(mailbox string) string {
	mailbox = strings.ToLower(strings.TrimSpace(mailbox))
	at := strings.LastIndex(mailbox, "@")
	if at < 0 || at+1 >= len(mailbox) {
		return ""
	}
	return mailbox[at+1:]
}

func (s *TempMailService) Configure(proxy string, cfg *TempMailConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proxy = strings.TrimSpace(proxy)
	if cfg == nil {
		s.createGap = tempMailDefaultGap
	} else {
		s.createGap = cfg.MailboxCreateGap()
	}
	if proxy != s.proxy {
		s.proxy = proxy
		// 代理变更后重建 HTTP 客户端（保留已缓存 token/mailbox）。
		s.httpClient = nil
	}
	s.apiBaseURL = tempmailLOLAPIBase
	if cfg != nil {
		s.apiBaseURL = cfg.TempmailLOLAPIBase()
	}
	// 新任务固定使用 tempmail.lol，避免沿用上一轮任务残留状态。
	s.provider = ""
	s.token = ""
	s.currentMailbox = ""
	s.detailCache = nil
	s.firstServed = false
	s.freshOnFirst = false
	s.blockedDomains = loadBlockedTempMailDomains()
}

func (s *TempMailService) EnsureReady() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureReadyLocked()
}

func (s *TempMailService) ensureReadyLocked() error {
	if s.httpClient == nil {
		client, err := NewHTTPClient(s.proxy)
		if err != nil {
			return fmt.Errorf("创建 Temp Mail HTTP 客户端失败: %w", err)
		}
		s.httpClient = client
	}

	hasReadyMailbox := isValidMailbox(s.currentMailbox) && s.token != ""
	if hasReadyMailbox {
		if s.provider == "" {
			s.provider = "tempmail-lol"
		}
		s.freshOnFirst = true
		return nil
	}

	if s.loadSessionLocked() {
		if s.validateCurrentMailboxLocked() {
			s.firstServed = false
			s.freshOnFirst = true
			return nil
		}
		s.provider = ""
		s.token = ""
		s.currentMailbox = ""
		s.detailCache = nil
	}
	if err := s.createOrRotateMailboxLocked(""); err != nil {
		return err
	}
	s.firstServed = false
	s.freshOnFirst = false
	return nil
}

func (s *TempMailService) MarkRejectedMailbox(mailbox, reason string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	domain := mailboxDomain(mailbox)
	if domain == "" {
		return ""
	}
	if s.blockedDomains == nil {
		s.blockedDomains = make(map[string]string)
	}
	s.blockedDomains[domain] = strings.TrimSpace(reason)
	if err := persistBlockedTempMailDomain(domain, reason); err != nil {
		log.Printf("[warning] %s", localizeRuntimeText(activeLanguage(), fmt.Sprintf("持久化临时邮箱域名失败: %v", err)))
	}

	if strings.EqualFold(mailboxDomain(s.currentMailbox), domain) {
		s.currentMailbox = ""
		s.detailCache = nil
		if strings.EqualFold(s.provider, "tempmail-lol") {
			s.token = ""
		}
	}
	return domain
}

func (s *TempMailService) isBlockedDomainLocked(domain string) bool {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" || len(s.blockedDomains) == 0 {
		return false
	}
	_, blocked := s.blockedDomains[domain]
	return blocked
}

func (s *TempMailService) AcquireMailbox() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReadyLocked(); err != nil {
		return "", err
	}

	if !s.firstServed {
		s.firstServed = true
		if s.freshOnFirst {
			// 新任务的首个账号也强制拿新邮箱，避免重启后复用上一个 mailbox。
			if err := s.createFreshMailboxLocked(s.token); err == nil {
				s.freshOnFirst = false
				return s.currentMailbox, nil
			} else if s.freshOnFirst {
				return "", err
			}
		}
		s.freshOnFirst = false
		return s.currentMailbox, nil
	}

	if err := s.createFreshMailboxLocked(s.token); err != nil {
		return "", err
	}
	return s.currentMailbox, nil
}

func (s *TempMailService) createFreshMailboxLocked(authToken string) error {
	previousMailbox := strings.TrimSpace(strings.ToLower(s.currentMailbox))
	const maxAttempts = 8

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := s.createOrRotateMailboxLocked(authToken); err != nil {
			return err
		}
		authToken = s.token

		currentMailbox := strings.TrimSpace(strings.ToLower(s.currentMailbox))
		if previousMailbox != "" && strings.EqualFold(previousMailbox, currentMailbox) {
			lastErr = fmt.Errorf("未获取到新的临时邮箱，已阻止复用旧地址: %s", previousMailbox)
		}

		domain := mailboxDomain(currentMailbox)
		if currentMailbox != "" && !s.isBlockedDomainLocked(domain) && !strings.EqualFold(previousMailbox, currentMailbox) {
			return nil
		}

		if currentMailbox != "" && s.isBlockedDomainLocked(domain) {
			reason := truncate(s.blockedDomains[domain], 80)
			if reason == "" {
				reason = "该域名已在本地数据库中标记为不可用"
			}
			broadcast(fmt.Sprintf("    ⚠️ 跳过数据库中已标记不可用的临时邮箱域名: %s (%s)", domain, reason), "warning")
			s.currentMailbox = ""
			s.detailCache = nil
			s.token = ""
			lastErr = fmt.Errorf("临时邮箱域名已在本地数据库中标记为不可用: %s", domain)
		}
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("本地数据库中没有可用的临时邮箱域名")
}

func (s *TempMailService) createOrRotateMailboxLocked(authToken string) error {
	_ = authToken
	if wait := s.createGap - time.Since(s.lastCreatedAt); wait > 0 {
		broadcast(fmt.Sprintf("    ⏳ tempmail.lol 冷却中，等待 %ds...", int(wait.Seconds())+1), "dim")
		time.Sleep(wait)
	}
	if err := s.createTempmailLOLMailboxLocked(); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "限流") || strings.Contains(err.Error(), "429") {
			return fmt.Errorf("%w（建议先将 Temp Mail 数量设为 1，或为任务设置代理/VPN 后重试）", err)
		}
		return err
	}
	return nil
}

func (s *TempMailService) tempmailLOLGetLocked(path string, query url.Values) (int, string, error) {
	if s.httpClient == nil {
		return 0, "", fmt.Errorf("temp-mail client is nil")
	}
	base := strings.TrimRight(strings.TrimSpace(s.apiBaseURL), "/")
	if base == "" {
		base = tempmailLOLAPIBase
	}
	rawURL := base + path
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}
	req, err := fhttp.NewRequest("GET", rawURL, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header = fhttp.Header{
		"user-agent":      {s.httpClient.userAgent},
		"accept":          {"application/json"},
		"accept-language": {"en-US,en;q=0.9"},
		"accept-encoding": {"gzip, deflate, br"},
	}
	resp, err := s.httpClient.client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	s.httpClient.saveCookies(resp)
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

func (s *TempMailService) tempmailLOLPostJSONLocked(path string, payload interface{}) (int, string, error) {
	if s.httpClient == nil {
		return 0, "", fmt.Errorf("temp-mail client is nil")
	}
	base := strings.TrimRight(strings.TrimSpace(s.apiBaseURL), "/")
	if base == "" {
		base = tempmailLOLAPIBase
	}
	b, _ := json.Marshal(payload)
	req, err := fhttp.NewRequest("POST", base+path, strings.NewReader(string(b)))
	if err != nil {
		return 0, "", err
	}
	req.Header = fhttp.Header{
		"user-agent":      {s.httpClient.userAgent},
		"accept":          {"application/json"},
		"content-type":    {"application/json"},
		"accept-language": {"en-US,en;q=0.9"},
		"accept-encoding": {"gzip, deflate, br"},
	}
	resp, err := s.httpClient.client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	s.httpClient.saveCookies(resp)
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), nil
}

func decodeTempmailLOLInbox(body string, fallbackMailbox string) (string, []tempMailRow, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return "", nil, fmt.Errorf("解析 tempmail.lol 收件箱失败: %w", err)
	}
	if len(raw) == 0 {
		return "", nil, fmt.Errorf("tempmail.lol 收件箱为空或已过期")
	}

	mailbox := strings.TrimSpace(pickFirstNonEmpty(
		strFromAny(raw["address"]),
		strFromAny(raw["mailbox"]),
		fallbackMailbox,
	))

	rawEmails, _ := raw["emails"].([]interface{})
	rows := make([]tempMailRow, 0, len(rawEmails))
	for idx, item := range rawEmails {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		row := tempMailRow{
			ID: strings.TrimSpace(pickFirstNonEmpty(
				strFromAny(msg["id"]),
				strFromAny(msg["_id"]),
				strFromAny(msg["date"]),
			)),
			Received: strings.TrimSpace(pickFirstNonEmpty(
				strFromAny(msg["date"]),
				strFromAny(msg["created_at"]),
				strFromAny(msg["createdAt"]),
				strFromAny(msg["timestamp"]),
			)),
		}
		if row.ID == "" {
			row.ID = fmt.Sprintf("row-%d", idx)
		}
		if b, err := json.Marshal(msg); err == nil {
			row.Text = string(b)
		}
		rows = append(rows, row)
	}
	return mailbox, rows, nil
}

func (s *TempMailService) fetchRowsLocked() (string, []tempMailRow, error) {
	return s.fetchRowsTempmailLOLLocked()
}

func (s *TempMailService) createTempmailLOLMailboxLocked() error {
	status, body, err := s.tempmailLOLPostJSONLocked("/inbox/create", map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("请求 tempmail.lol mailbox 失败: %w", err)
	}
	if status != 200 && status != 201 {
		return fmt.Errorf("请求 tempmail.lol mailbox 失败: %d %s", status, truncate(body, 200))
	}

	var resp tempmailLOLMailboxResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return fmt.Errorf("解析 tempmail.lol mailbox 失败: %w", err)
	}
	resp.Address = strings.TrimSpace(resp.Address)
	resp.Token = strings.TrimSpace(resp.Token)
	if !isValidMailbox(resp.Address) || resp.Token == "" {
		return fmt.Errorf("tempmail.lol 未返回可用邮箱")
	}

	s.provider = "tempmail-lol"
	s.token = resp.Token
	s.currentMailbox = resp.Address
	s.lastCreatedAt = time.Now()
	s.detailCache = nil
	s.saveSessionLocked()
	return nil
}

func (s *TempMailService) fetchRowsTempmailLOLLocked() (string, []tempMailRow, error) {
	if strings.TrimSpace(s.token) == "" {
		return "", nil, fmt.Errorf("tempmail.lol token 为空")
	}
	query := url.Values{"token": []string{strings.TrimSpace(s.token)}}
	status, body, err := s.tempmailLOLGetLocked("/inbox", query)
	if err != nil {
		return "", nil, fmt.Errorf("读取 tempmail.lol 消息失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return "", nil, fmt.Errorf("读取 tempmail.lol 消息失败: %d %s", status, truncate(body, 200))
	}

	mailbox, rows, err := decodeTempmailLOLInbox(body, s.currentMailbox)
	if err != nil {
		return "", nil, err
	}
	if isValidMailbox(mailbox) {
		s.currentMailbox = mailbox
		s.saveSessionLocked()
	}
	return mailbox, rows, nil
}

func (s *TempMailService) validateCurrentMailboxLocked() bool {
	if !isValidMailbox(s.currentMailbox) || s.httpClient == nil {
		return false
	}
	if s.isBlockedDomainLocked(mailboxDomain(s.currentMailbox)) {
		return false
	}
	if strings.TrimSpace(s.token) == "" {
		return false
	}
	query := url.Values{"token": []string{strings.TrimSpace(s.token)}}
	status, _, err := s.tempmailLOLGetLocked("/inbox", query)
	if err != nil {
		return false
	}
	return status >= 200 && status < 300
}

func (s *TempMailService) sessionFilePathLocked() string {
	base := "."
	if strings.TrimSpace(resultsDir) != "" {
		base = strings.TrimSpace(resultsDir)
	}
	return filepath.Join(base, ".temp_mail_session.json")
}

func (s *TempMailService) loadSessionLocked() bool {
	path := s.sessionFilePathLocked()
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return false
	}
	var sess tempMailSession
	if err := json.Unmarshal(b, &sess); err != nil {
		return false
	}
	if provider := strings.TrimSpace(sess.Provider); provider != "" && !strings.EqualFold(provider, "tempmail-lol") {
		return false
	}
	s.provider = "tempmail-lol"
	sess.Token = strings.TrimSpace(sess.Token)
	sess.Mailbox = strings.TrimSpace(sess.Mailbox)
	if !isValidMailbox(sess.Mailbox) {
		return false
	}
	if sess.Token == "" {
		return false
	}
	s.token = sess.Token
	s.currentMailbox = sess.Mailbox
	return true
}

func (s *TempMailService) saveSessionLocked() {
	if !isValidMailbox(s.currentMailbox) {
		return
	}
	if strings.TrimSpace(s.token) == "" {
		return
	}
	path := s.sessionFilePathLocked()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	payload := tempMailSession{
		Provider:  "tempmail-lol",
		Token:     strings.TrimSpace(s.token),
		Mailbox:   strings.TrimSpace(s.currentMailbox),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0644)
}

func (s *TempMailService) FindCode(expectedEmail string, minTime time.Time, seen map[string]struct{}) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReadyLocked(); err != nil {
		return "", err
	}

	mailbox, rows, err := s.fetchRowsLocked()
	if err != nil {
		return "", err
	}
	if expectedEmail != "" && isValidMailbox(mailbox) && !strings.EqualFold(expectedEmail, mailbox) {
		return "", fmt.Errorf("tempmail.lol 当前邮箱变更: expected=%s current=%s", expectedEmail, mailbox)
	}

	return findBestTempMailCode(rows, minTime, seen), nil
}

func waitForTempMailCode(email string, otpSentAt time.Time, resendFn func() bool, waitMode otpWaitMode) (string, error) {
	integratedIMAP.ConsumeCode(strings.ToLower(email), "")
	minTime := otpMinTime(otpSentAt, waitMode)

	done := make(chan struct{})
	defer close(done)

	// 定时重发 OTP
	go func() {
		select {
		case <-time.After(20 * time.Second):
		case <-done:
			return
		}
		if resendFn != nil {
			if resendFn() {
				broadcast("    🔄 已重发 OTP", "info")
			}
		}
		ticker := time.NewTicker(ResendInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if resendFn != nil {
					if resendFn() {
						broadcast("    🔄 已重发 OTP", "info")
					}
				}
			case <-done:
				return
			}
		}
	}()

	// Temp Mail 轮询，发现验证码后注入到统一等待通道（支持终端手动输入兜底）
	go func() {
		seen := map[string]struct{}{}
		var lastWarnAt time.Time

		poll := func() {
			code, err := tempMailService.FindCode(email, minTime, seen)
			if err != nil {
				if time.Since(lastWarnAt) > 10*time.Second {
					broadcast(fmt.Sprintf("    ⚠️ Temp Mail 轮询异常: %s", truncate(err.Error(), 120)), "warning")
					lastWarnAt = time.Now()
				}
				return
			}
			if code == "" {
				return
			}
			if _, injectErr := integratedIMAP.InjectManualCode(email, code, "tempmail.lol"); injectErr != nil {
				if time.Since(lastWarnAt) > 10*time.Second {
					broadcast(fmt.Sprintf("    ⚠️ Temp Mail 注入验证码失败: %s", truncate(injectErr.Error(), 120)), "warning")
					lastWarnAt = time.Now()
				}
			}
		}

		poll()
		ticker := time.NewTicker(tempMailPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				poll()
			}
		}
	}()

	manualCodeHint(email)
	code, err := WaitVerificationCode(email, PollTimeout, minTime)
	if err != nil {
		return "", err
	}
	if code == "" {
		return "", fmt.Errorf("empty verification code for %s", email)
	}
	broadcast(fmt.Sprintf("    ✅ 验证码: %s (Temp Mail)", code), "success")
	return code, nil
}

func extractTempMailCode(text string, _ int) string {
	if text == "" {
		return ""
	}

	// 去掉邮箱地址，避免误把地址中的 6 位数字当作验证码
	clean := tempMailEmailRe.ReplaceAllString(text, " ")
	lower := strings.ToLower(clean)
	if !strings.Contains(lower, "chatgpt") {
		return ""
	}

	if m := tempMailChatGPTCodeRe.FindStringSubmatch(clean); len(m) > 1 {
		return m[1]
	}

	pos := strings.Index(lower, "chatgpt")
	if pos < 0 || pos >= len(clean) {
		return ""
	}
	tail := clean[pos:]
	if m := tempMailAfterCodeRe.FindStringSubmatch(tail); len(m) > 1 {
		return m[1]
	}
	if m := tempMailCodeRe.FindStringSubmatch(tail); len(m) > 1 {
		return m[1]
	}

	return ""
}

func isTempMailCodeCandidate(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "chatgpt") || strings.Contains(lower, "openai")
}

func findBestTempMailCode(rows []tempMailRow, minTime time.Time, seen map[string]struct{}) string {
	rowCount := len(rows)
	var bestCode string
	var bestTs time.Time

	for _, row := range rows {
		key := strings.TrimSpace(row.ID)
		if key == "" {
			key = row.Received + "|" + truncate(row.Text, 120)
		}
		if _, ok := seen[key]; ok {
			continue
		}

		ts := parseTempMailTime(row.Received)
		if !ts.IsZero() && ts.Before(minTime) {
			seen[key] = struct{}{}
			continue
		}

		code := extractTempMailCode(row.Text, rowCount)
		if code == "" {
			if !isTempMailCodeCandidate(row.Text) {
				seen[key] = struct{}{}
			}
			continue
		}
		seen[key] = struct{}{}

		if ts.IsZero() {
			if bestCode == "" {
				bestCode = code
			}
			continue
		}
		if bestTs.IsZero() || ts.After(bestTs) {
			bestTs = ts
			bestCode = code
		}
	}

	return bestCode
}

func parseTempMailTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}

	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n > 1_000_000_000_000 {
			return time.UnixMilli(n)
		}
		if n > 1_000_000_000 {
			return time.Unix(n, 0)
		}
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		n := int64(f)
		if n > 1_000_000_000_000 {
			return time.UnixMilli(n)
		}
		if n > 1_000_000_000 {
			return time.Unix(n, 0)
		}
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05 -0700",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

func isValidMailbox(mailbox string) bool {
	mailbox = strings.TrimSpace(strings.ToLower(mailbox))
	if mailbox == "" {
		return false
	}
	if !strings.Contains(mailbox, "@") {
		return false
	}
	if strings.Contains(mailbox, "loading") {
		return false
	}
	return true
}

func strFromAny(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

func pickFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func splitMailbox(mailbox string) (string, string, bool) {
	mailbox = strings.TrimSpace(strings.ToLower(mailbox))
	parts := strings.Split(mailbox, "@")
	if len(parts) != 2 {
		return "", "", false
	}
	login := strings.TrimSpace(parts[0])
	domain := strings.TrimSpace(parts[1])
	if login == "" || domain == "" {
		return "", "", false
	}
	return login, domain, true
}
