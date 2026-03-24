package main

import (
	"errors"
	"strings"
	"testing"
)

func TestParseAccountLineUsesFallbackRegisterPassword(t *testing.T) {
	acc, ok := parseAccountLine("user@outlook.com----mail-pass----cid----rt", "Qwer1234!Aa#")
	if !ok {
		t.Fatal("parseAccountLine() = false, want true")
	}
	if acc.Email != "user@outlook.com" {
		t.Fatalf("Email = %q, want %q", acc.Email, "user@outlook.com")
	}
	if acc.Password != "mail-pass" {
		t.Fatalf("Password = %q, want %q", acc.Password, "mail-pass")
	}
	if acc.RegisterPassword != "Qwer1234!Aa#" {
		t.Fatalf("RegisterPassword = %q, want %q", acc.RegisterPassword, "Qwer1234!Aa#")
	}
	if acc.ClientID != "cid" || acc.RefreshToken != "rt" {
		t.Fatalf("unexpected oauth fields: client_id=%q refresh_token=%q", acc.ClientID, acc.RefreshToken)
	}
}

func TestParseAccountLinePrefersExplicitRegisterPassword(t *testing.T) {
	acc, ok := parseAccountLine("user@outlook.com----mail-pass----cid----rt----CustomPass123!", "Qwer1234!Aa#")
	if !ok {
		t.Fatal("parseAccountLine() = false, want true")
	}
	if acc.RegisterPassword != "CustomPass123!" {
		t.Fatalf("RegisterPassword = %q, want %q", acc.RegisterPassword, "CustomPass123!")
	}
}

func TestResolveRegisterPassword(t *testing.T) {
	tests := []struct {
		name    string
		account Account
		want    string
	}{
		{
			name:    "uses explicit register password",
			account: Account{Password: "mail-pass", RegisterPassword: "CustomPass123!"},
			want:    "CustomPass123!",
		},
		{
			name:    "falls back to password for legacy accounts",
			account: Account{Password: "LegacyPass123!"},
			want:    "LegacyPass123!",
		},
		{
			name:    "upgrades legacy default",
			account: Account{Password: "Qwer1234!"},
			want:    defaultRegisterPassword,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveRegisterPassword(tt.account, nil, nil)
			if err != nil {
				t.Fatalf("resolveRegisterPassword() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveRegisterPassword() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveRegisterPasswordUsesDefaultWhenOutlookFallbackIsInjected(t *testing.T) {
	acc, ok := parseAccountLine("user@outlook.com----short", defaultRegisterPassword)
	if !ok {
		t.Fatal("parseAccountLine() = false, want true")
	}
	got, err := resolveRegisterPassword(acc, nil, nil)
	if err != nil {
		t.Fatalf("resolveRegisterPassword() unexpected error: %v", err)
	}
	if got != defaultRegisterPassword {
		t.Fatalf("resolveRegisterPassword() = %q, want %q", got, defaultRegisterPassword)
	}
}

func TestResolveRegisterPasswordDomainUsesAccountPassword(t *testing.T) {
	acc := Account{Password: "DomainPass123!"}
	got, err := resolveRegisterPassword(acc, &DomainMailConfig{Host: "imap.example.com"}, nil)
	if err != nil {
		t.Fatalf("resolveRegisterPassword() unexpected error: %v", err)
	}
	if got != "DomainPass123!" {
		t.Fatalf("resolveRegisterPassword() = %q, want %q", got, "DomainPass123!")
	}
}

func TestResolveRegisterPasswordOutlookUpgradesShortExplicitPassword(t *testing.T) {
	acc := Account{Password: "mail-pass", RegisterPassword: "short"}
	got, err := resolveRegisterPassword(acc, nil, nil)
	if err != nil {
		t.Fatalf("resolveRegisterPassword() unexpected error: %v", err)
	}
	if got != defaultRegisterPassword {
		t.Fatalf("resolveRegisterPassword() = %q, want %q", got, defaultRegisterPassword)
	}
}

func TestNormalizeOutlookRegisterPassword(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", defaultRegisterPassword},
		{"Qwer1234!", defaultRegisterPassword},
		{"short", defaultRegisterPassword},
		{"CustomPass123!", "CustomPass123!"},
	}

	for _, tt := range tests {
		if got := normalizeOutlookRegisterPassword(tt.in); got != tt.want {
			t.Fatalf("normalizeOutlookRegisterPassword(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatOutlookIMAPAuthError(t *testing.T) {
	err := formatOutlookIMAPAuthError(
		errors.New("invalid_grant"),
		errors.New("AUTHENTICATE failed"),
		errors.New("LOGIN failed"),
	)
	msg := err.Error()
	for _, want := range []string{
		"Outlook IMAP 认证失败",
		"刷新 MS Token 失败: invalid_grant",
		"XOAUTH2 认证失败: AUTHENTICATE failed",
		"密码 LOGIN 失败: LOGIN failed",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("formatOutlookIMAPAuthError() = %q, missing %q", msg, want)
		}
	}
}
