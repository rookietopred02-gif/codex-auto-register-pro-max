package main

import (
	"testing"
	"time"
)

func TestExtractTempMailCodeTrusted(t *testing.T) {
	row := `{"mail_from":"noreply@tm.openai.com","mail_subject":"你的 ChatGPT 代码为 039738","created_at":"2026-03-09T05:20:10Z"}`
	code := extractTempMailCode(row, 1)
	if code != "039738" {
		t.Fatalf("expected 039738, got %q", code)
	}
}

func TestExtractTempMailCodeUntrustedMultiRows(t *testing.T) {
	row := `{"mail_from":"ad@example.com","mail_subject":"Order 123456 confirmed"}`
	code := extractTempMailCode(row, 3)
	if code != "" {
		t.Fatalf("expected empty code for untrusted multi-row message, got %q", code)
	}
}

func TestExtractTempMailCodeOnlyAfterChatGPT(t *testing.T) {
	row := `{"mail_subject":"123456 is old value. Your ChatGPT code is 654321"}`
	code := extractTempMailCode(row, 1)
	if code != "654321" {
		t.Fatalf("expected 654321, got %q", code)
	}
}

func TestExtractTempMailCodeNoDigitsAfterChatGPT(t *testing.T) {
	row := `{"mail_subject":"123456 welcome to ChatGPT"}`
	code := extractTempMailCode(row, 1)
	if code != "" {
		t.Fatalf("expected empty code, got %q", code)
	}
}

func TestExtractTempMailCodeRejectsEmailAddressDigits(t *testing.T) {
	row := `foo123456@example.com ChatGPT verification`
	code := extractTempMailCode(row, 1)
	if code != "" {
		t.Fatalf("expected empty code, got %q", code)
	}
}

func TestParseTempMailTimeUnixMillis(t *testing.T) {
	got := parseTempMailTime("1773004905406")
	if got.IsZero() {
		t.Fatalf("expected parsed time, got zero")
	}
	if got.Year() < 2025 {
		t.Fatalf("unexpected parsed year: %d", got.Year())
	}
}

func TestParseTempMailTimeRFC3339(t *testing.T) {
	got := parseTempMailTime("2026-03-09T05:20:10Z")
	want := time.Date(2026, 3, 9, 5, 20, 10, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestFindBestTempMailCodeRechecksCandidateWithoutCode(t *testing.T) {
	seen := map[string]struct{}{}
	minTime := time.Date(2026, 3, 9, 5, 20, 0, 0, time.UTC)

	rows1 := []tempMailRow{
		{
			ID:       "msg-1",
			Received: "2026-03-09T05:20:10Z",
			Text:     `{"mail_subject":"Your ChatGPT code is pending","mail_from":"noreply@tm.openai.com"}`,
		},
	}
	if got := findBestTempMailCode(rows1, minTime, seen); got != "" {
		t.Fatalf("expected empty code on first pass, got %q", got)
	}
	if _, ok := seen["msg-1"]; ok {
		t.Fatalf("candidate row without code should not be marked seen yet")
	}

	rows2 := []tempMailRow{
		{
			ID:       "msg-1",
			Received: "2026-03-09T05:20:10Z",
			Text:     `{"mail_subject":"Your ChatGPT code is 654321","mail_from":"noreply@tm.openai.com"}`,
		},
	}
	if got := findBestTempMailCode(rows2, minTime, seen); got != "654321" {
		t.Fatalf("expected code after recheck, got %q", got)
	}
	if _, ok := seen["msg-1"]; !ok {
		t.Fatalf("row with code should be marked seen")
	}
}

func TestFindBestTempMailCodeMarksNonCandidateSeen(t *testing.T) {
	seen := map[string]struct{}{}
	minTime := time.Date(2026, 3, 9, 5, 20, 0, 0, time.UTC)

	rows := []tempMailRow{
		{
			ID:       "spam-1",
			Received: "2026-03-09T05:20:10Z",
			Text:     `{"mail_subject":"Order 123456 confirmed","mail_from":"ad@example.com"}`,
		},
	}
	if got := findBestTempMailCode(rows, minTime, seen); got != "" {
		t.Fatalf("expected empty code, got %q", got)
	}
	if _, ok := seen["spam-1"]; !ok {
		t.Fatalf("non-candidate row should be marked seen")
	}
}

func TestFindBestTempMailCodeStrictMinTimeSkipsOlderOTP(t *testing.T) {
	seen := map[string]struct{}{}
	minTime := time.Date(2026, 3, 24, 21, 45, 19, 0, time.UTC)

	rows := []tempMailRow{
		{
			ID:       "otp-old",
			Received: "2026-03-24T21:45:03Z",
			Text:     `{"mail_subject":"Your ChatGPT code is 508698","mail_from":"noreply@tm.openai.com"}`,
		},
		{
			ID:       "otp-new",
			Received: "2026-03-24T21:45:21Z",
			Text:     `{"mail_subject":"Your ChatGPT code is 113294","mail_from":"noreply@tm.openai.com"}`,
		},
	}

	if got := findBestTempMailCode(rows, minTime, seen); got != "113294" {
		t.Fatalf("expected newest OTP after minTime, got %q", got)
	}
}

func TestIsTempMailCodeCandidate(t *testing.T) {
	if !isTempMailCodeCandidate("ChatGPT security code") {
		t.Fatal("expected ChatGPT text to be candidate")
	}
	if isTempMailCodeCandidate("newsletter") {
		t.Fatal("did not expect unrelated text to be candidate")
	}
}

func TestTempMailConfigureResetsTaskFlags(t *testing.T) {
	delay0 := 0
	svc := &TempMailService{
		provider:     "tempmail-lol",
		firstServed:  true,
		freshOnFirst: true,
		createGap:    30 * time.Second,
	}

	svc.Configure("", &TempMailConfig{NextDelaySeconds: &delay0})

	if svc.firstServed {
		t.Fatalf("expected firstServed to reset for new task")
	}
	if svc.freshOnFirst {
		t.Fatalf("expected freshOnFirst to reset for new task")
	}
	if svc.provider != "" {
		t.Fatalf("expected provider to reset for new task, got %q", svc.provider)
	}
	if svc.createGap != 0 {
		t.Fatalf("expected createGap to follow config, got %s", svc.createGap)
	}
}

func TestMarkRejectedMailboxBlocksDomainAndClearsActiveMailbox(t *testing.T) {
	svc := &TempMailService{
		provider:       "tempmail-lol",
		token:          "tok",
		currentMailbox: "user@sharebot.net",
		blockedDomains: map[string]string{},
	}

	got := svc.MarkRejectedMailbox("user@sharebot.net", "create account rejected")
	if got != "sharebot.net" {
		t.Fatalf("got %q want sharebot.net", got)
	}
	if !svc.isBlockedDomainLocked("sharebot.net") {
		t.Fatal("expected sharebot.net to be blocked")
	}
	if svc.currentMailbox != "" {
		t.Fatalf("expected current mailbox cleared, got %q", svc.currentMailbox)
	}
	if svc.token != "" {
		t.Fatalf("expected tempmail.lol token cleared, got %q", svc.token)
	}
}

func TestDecodeTempmailLOLInbox(t *testing.T) {
	body := `{"address":"demo@tempmail.lol","token":"tok-1","emails":[{"date":"2026-03-24T10:00:00Z","from":"noreply@tm.openai.com","subject":"Your ChatGPT code is 654321","body":"654321"}]}`

	mailbox, rows, err := decodeTempmailLOLInbox(body, "")
	if err != nil {
		t.Fatalf("decodeTempmailLOLInbox() unexpected error: %v", err)
	}
	if mailbox != "demo@tempmail.lol" {
		t.Fatalf("mailbox = %q, want demo@tempmail.lol", mailbox)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].Received != "2026-03-24T10:00:00Z" {
		t.Fatalf("rows[0].Received = %q", rows[0].Received)
	}
	if extractTempMailCode(rows[0].Text, len(rows)) != "654321" {
		t.Fatalf("expected parsed code 654321, got %q", extractTempMailCode(rows[0].Text, len(rows)))
	}
}
