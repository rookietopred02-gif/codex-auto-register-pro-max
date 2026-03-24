package main

import (
	"testing"
	"time"
)

func TestNormalizeTempWorkers(t *testing.T) {
	cases := []struct {
		name          string
		requested     int
		allowParallel bool
		want          int
	}{
		{name: "off forces single", requested: 8, allowParallel: false, want: 1},
		{name: "on keeps valid", requested: 4, allowParallel: true, want: 4},
		{name: "on clamps low", requested: 0, allowParallel: true, want: 1},
		{name: "on clamps high", requested: 99, allowParallel: true, want: 50},
	}

	for _, tc := range cases {
		got := normalizeTempWorkers(tc.requested, tc.allowParallel)
		if got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

func TestTempMailPostSuccessDelaySeconds(t *testing.T) {
	delay0 := 0
	delay12 := 12
	delayHigh := 999
	delayNeg := -3

	cases := []struct {
		name string
		cfg  *TempMailConfig
		want int
	}{
		{name: "nil config uses default", cfg: nil, want: 15},
		{name: "missing value uses default", cfg: &TempMailConfig{}, want: 15},
		{name: "zero means no wait", cfg: &TempMailConfig{NextDelaySeconds: &delay0}, want: 0},
		{name: "keeps valid value", cfg: &TempMailConfig{NextDelaySeconds: &delay12}, want: 12},
		{name: "clamps high", cfg: &TempMailConfig{NextDelaySeconds: &delayHigh}, want: 300},
		{name: "negative falls back to default", cfg: &TempMailConfig{NextDelaySeconds: &delayNeg}, want: 15},
	}

	for _, tc := range cases {
		got := tc.cfg.PostSuccessDelaySeconds()
		if got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

func TestTempMailMailboxCreateGap(t *testing.T) {
	delay0 := 0
	delay7 := 7

	cases := []struct {
		name string
		cfg  *TempMailConfig
		want time.Duration
	}{
		{name: "nil config uses default gap", cfg: nil, want: 15 * time.Second},
		{name: "missing value uses default gap", cfg: &TempMailConfig{}, want: 15 * time.Second},
		{name: "zero means immediate rotate", cfg: &TempMailConfig{NextDelaySeconds: &delay0}, want: 0},
		{name: "uses configured delay", cfg: &TempMailConfig{NextDelaySeconds: &delay7}, want: 7 * time.Second},
	}

	for _, tc := range cases {
		got := tc.cfg.MailboxCreateGap()
		if got != tc.want {
			t.Fatalf("%s: got %s want %s", tc.name, got, tc.want)
		}
	}
}

func TestExtractPageTypeDetailedFallbacks(t *testing.T) {
	if got := extractPageTypeDetailed(nil, "https://auth.openai.com/add-phone", ""); got != "add_phone" {
		t.Fatalf("expected add_phone from url, got %q", got)
	}
	body := `{"error":{"message":"cannot create your account with the given information"}}`
	if got := extractPageTypeDetailed(nil, "", body); got != "registration_disallowed" {
		t.Fatalf("expected registration_disallowed from body, got %q", got)
	}
}

func TestNormalizeRegisterPassword(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "blank uses default", input: "", want: defaultRegisterPassword},
		{name: "legacy default is upgraded", input: "Qwer1234!", want: defaultRegisterPassword},
		{name: "valid password is kept", input: "Abcd1234!XYZ", want: "Abcd1234!XYZ"},
		{name: "short custom password is rejected", input: "short123!", wantErr: true},
	}

	for _, tc := range cases {
		got, err := normalizeRegisterPassword(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error", tc.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestAdjustedFlowDelayRange(t *testing.T) {
	minMs, maxMs := adjustedFlowDelayRange(false, 800, 2000)
	if minMs != 800 || maxMs != 2000 {
		t.Fatalf("non-temp mode should keep original delays, got %d-%d", minMs, maxMs)
	}

	minMs, maxMs = adjustedFlowDelayRange(true, 800, 2000)
	if minMs != 200 || maxMs != 500 {
		t.Fatalf("temp mode should shrink delays, got %d-%d", minMs, maxMs)
	}

	minMs, maxMs = adjustedFlowDelayRange(true, 100, 120)
	if minMs < 80 || maxMs < minMs {
		t.Fatalf("temp mode should keep a valid clamped range, got %d-%d", minMs, maxMs)
	}
}

func TestShouldRotateTempMailboxOnRetry(t *testing.T) {
	tempCfg := &TempMailConfig{Count: 1}

	cases := []struct {
		name    string
		attempt int
		cfg     *TempMailConfig
		errMsg  string
		want    bool
	}{
		{name: "nil temp mail disables rotate", attempt: 1, cfg: nil, errMsg: "创建账号失败", want: false},
		{name: "last attempt does not rotate", attempt: MaxRetry, cfg: tempCfg, errMsg: "创建账号失败", want: false},
		{name: "create account rejection rotates", attempt: 1, cfg: tempCfg, errMsg: "创建账号失败: 400 cannot create your account with the given information", want: true},
		{name: "add phone blocker rotates", attempt: 1, cfg: tempCfg, errMsg: "账户创建后进入 add_phone 流程，当前尚未生成 workspace", want: true},
		{name: "username rejection rotates", attempt: 1, cfg: tempCfg, errMsg: "提交注册密码失败: failed to register username", want: true},
		{name: "unrelated error does not rotate", attempt: 1, cfg: tempCfg, errMsg: "network timeout", want: false},
	}

	for _, tc := range cases {
		if got := shouldRotateTempMailboxOnRetry(tc.attempt, tc.cfg, tc.errMsg); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestOTPMinTime(t *testing.T) {
	sentAt := time.Date(2026, 3, 24, 21, 45, 19, 0, time.UTC)

	if got := otpMinTime(sentAt, otpWaitAllowClockSkew); !got.Equal(sentAt.Add(-60 * time.Second)) {
		t.Fatalf("allow skew min time = %s, want %s", got, sentAt.Add(-60*time.Second))
	}
	if got := otpMinTime(sentAt, otpWaitRequireFreshCode); !got.Equal(sentAt) {
		t.Fatalf("fresh code min time = %s, want %s", got, sentAt)
	}
}

func TestShouldReloginAfterCreateAccount(t *testing.T) {
	cases := []struct {
		name       string
		isExisting bool
		isLogin    bool
		want       bool
	}{
		{name: "new account in register mode stays on post-create flow", isExisting: false, isLogin: false, want: false},
		{name: "existing account in login mode does not relogin after create", isExisting: true, isLogin: true, want: false},
		{name: "new account in login mode switches to relogin flow", isExisting: false, isLogin: true, want: true},
	}

	for _, tc := range cases {
		if got := shouldReloginAfterCreateAccount(tc.isExisting, tc.isLogin); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestShouldRejectTempMailDomain(t *testing.T) {
	cases := []struct {
		name   string
		errMsg string
		want   bool
	}{
		{name: "create account rejection is domain level", errMsg: "创建账号失败: 400 cannot create your account with the given information", want: true},
		{name: "registration disallowed is domain level", errMsg: "registration_disallowed", want: true},
		{name: "otp failure is not domain level", errMsg: "OTP 验证失败", want: false},
		{name: "empty is false", errMsg: "", want: false},
	}

	for _, tc := range cases {
		if got := shouldRejectTempMailDomain(tc.errMsg); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
