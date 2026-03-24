package main

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func TestExtractWorkspaceID(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{
			name: "nil map",
			data: nil,
			want: "",
		},
		{
			name: "top level workspace_id",
			data: map[string]interface{}{"workspace_id": "ws-top"},
			want: "ws-top",
		},
		{
			name: "top level default_workspace_id",
			data: map[string]interface{}{"default_workspace_id": "ws-default"},
			want: "ws-default",
		},
		{
			name: "nested workspace id",
			data: map[string]interface{}{
				"workspace": map[string]interface{}{"id": "ws-nested"},
			},
			want: "ws-nested",
		},
		{
			name: "nested selected workspace id field",
			data: map[string]interface{}{
				"selected_workspace": map[string]interface{}{"workspace_id": "ws-selected"},
			},
			want: "ws-selected",
		},
		{
			name: "first workspaces entry id",
			data: map[string]interface{}{
				"workspaces": []interface{}{
					map[string]interface{}{"id": "ws-array"},
				},
			},
			want: "ws-array",
		},
		{
			name: "first workspaces entry workspace_id",
			data: map[string]interface{}{
				"workspaces": []interface{}{
					map[string]interface{}{"workspace_id": "ws-array-alt"},
				},
			},
			want: "ws-array-alt",
		},
		{
			name: "skip invalid workspace entries",
			data: map[string]interface{}{
				"workspaces": []interface{}{
					"bad",
					map[string]interface{}{"id": ""},
					map[string]interface{}{"workspace_id": "ws-valid"},
				},
			},
			want: "ws-valid",
		},
		{
			name: "no workspace fields",
			data: map[string]interface{}{"foo": "bar"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractWorkspaceID(tt.data); got != tt.want {
				t.Fatalf("extractWorkspaceID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSortedMapKeys(t *testing.T) {
	if got := sortedMapKeys(nil); got != nil {
		t.Fatalf("sortedMapKeys(nil) = %#v, want nil", got)
	}

	data := map[string]interface{}{
		"zeta":  1,
		"alpha": 2,
		"mid":   3,
	}
	want := []string{"alpha", "mid", "zeta"}

	if got := sortedMapKeys(data); !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedMapKeys() = %#v, want %#v", got, want)
	}
}

func TestResolveWorkspaceIDPrefersCreateAccountData(t *testing.T) {
	createAccountData := map[string]interface{}{
		"workspace": map[string]interface{}{
			"id": "ws-from-create-account",
		},
	}

	workspaceID, cookieData, err := resolveWorkspaceID("", createAccountData)
	if err != nil {
		t.Fatalf("resolveWorkspaceID() unexpected error: %v", err)
	}
	if workspaceID != "ws-from-create-account" {
		t.Fatalf("resolveWorkspaceID() workspaceID = %q, want %q", workspaceID, "ws-from-create-account")
	}
	if cookieData != nil {
		t.Fatalf("resolveWorkspaceID() cookieData = %#v, want nil when create_account already has workspace", cookieData)
	}
}

func TestResolveWorkspaceIDFallsBackToCookie(t *testing.T) {
	payload := []byte(`{"workspaces":[{"id":"ws-from-cookie"}]}`)
	authCookie := base64.RawURLEncoding.EncodeToString(payload) + ".sig"

	workspaceID, cookieData, err := resolveWorkspaceID(authCookie, map[string]interface{}{"page": "about-you"})
	if err != nil {
		t.Fatalf("resolveWorkspaceID() unexpected error: %v", err)
	}
	if workspaceID != "ws-from-cookie" {
		t.Fatalf("resolveWorkspaceID() workspaceID = %q, want %q", workspaceID, "ws-from-cookie")
	}
	if got := extractWorkspaceID(cookieData); got != "ws-from-cookie" {
		t.Fatalf("resolveWorkspaceID() cookie workspace = %q, want %q", got, "ws-from-cookie")
	}
}

func TestResolveWorkspaceIDUsesPostCreateContinueDataBeforeCookie(t *testing.T) {
	payload := []byte(`{"workspaces":[{"id":"ws-from-cookie"}]}`)
	authCookie := base64.RawURLEncoding.EncodeToString(payload) + ".sig"

	postCreateContinueData := map[string]interface{}{
		"workspaces": []interface{}{
			map[string]interface{}{"id": "ws-from-post-create-continue"},
		},
	}

	workspaceID, cookieData, err := resolveWorkspaceID(
		authCookie,
		map[string]interface{}{"page": "about-you"},
		postCreateContinueData,
	)
	if err != nil {
		t.Fatalf("resolveWorkspaceID() unexpected error: %v", err)
	}
	if workspaceID != "ws-from-post-create-continue" {
		t.Fatalf("resolveWorkspaceID() workspaceID = %q, want %q", workspaceID, "ws-from-post-create-continue")
	}
	if cookieData != nil {
		t.Fatalf("resolveWorkspaceID() cookieData = %#v, want nil when post-create continue data already has workspace", cookieData)
	}
}

func TestResolveWorkspaceIDReturnsCookieDecodeError(t *testing.T) {
	_, _, err := resolveWorkspaceID("not-base64.sig", map[string]interface{}{"page": "about-you"})
	if err == nil {
		t.Fatal("resolveWorkspaceID() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "解析 cookie 失败") && !strings.Contains(err.Error(), "解析 cookie JSON 失败") {
		t.Fatalf("resolveWorkspaceID() error = %q, want cookie decode/json context", err.Error())
	}
}

func TestResolveWorkspaceIDReturnsMissingCookieErrorWhenFallbackNeeded(t *testing.T) {
	_, _, err := resolveWorkspaceID("", map[string]interface{}{"page": "about-you"})
	if err == nil {
		t.Fatal("resolveWorkspaceID() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "oai-client-auth-session") {
		t.Fatalf("resolveWorkspaceID() error = %q, want missing-cookie context", err.Error())
	}
}
