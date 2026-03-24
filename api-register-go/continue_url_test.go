package main

import (
	"strings"
	"testing"
)

func TestExtractContinueURL(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{
			name: "top level continue_url",
			data: map[string]interface{}{"continue_url": "https://example.com/next"},
			want: "https://example.com/next",
		},
		{
			name: "nested page continueUrl",
			data: map[string]interface{}{
				"page": map[string]interface{}{"continueUrl": "https://example.com/page"},
			},
			want: "https://example.com/page",
		},
		{
			name: "nested result redirect_url",
			data: map[string]interface{}{
				"result": map[string]interface{}{"redirect_url": "https://example.com/redirect"},
			},
			want: "https://example.com/redirect",
		},
		{
			name: "no continue url",
			data: map[string]interface{}{"foo": "bar"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractContinueURL(tt.data); got != tt.want {
				t.Fatalf("extractContinueURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodeJSONMapBody(t *testing.T) {
	if got := decodeJSONMapBody(`{"workspace":{"id":"ws-123"}}`); extractWorkspaceID(got) != "ws-123" {
		t.Fatalf("decodeJSONMapBody() workspace = %q, want %q", extractWorkspaceID(got), "ws-123")
	}

	if got := decodeJSONMapBody(`<html>not json</html>`); got != nil {
		t.Fatalf("decodeJSONMapBody() = %#v, want nil for non-JSON body", got)
	}
}

func TestFormatWorkspaceSourceKeys(t *testing.T) {
	got := formatWorkspaceSourceKeys(
		map[string]interface{}{"page": "about-you", "workspace_id": "ws-1"},
		map[string]interface{}{"step": "consent"},
	)
	if !strings.Contains(got, "create_account keys=page,workspace_id") {
		t.Fatalf("formatWorkspaceSourceKeys() = %q, want create_account keys", got)
	}
	if !strings.Contains(got, "post_create_continue keys=step") {
		t.Fatalf("formatWorkspaceSourceKeys() = %q, want post_create_continue keys", got)
	}
}

func TestExtractWorkspaceBlockerPageType(t *testing.T) {
	got := extractWorkspaceBlockerPageType(
		map[string]interface{}{
			"page": map[string]interface{}{"type": "add_phone"},
		},
		map[string]interface{}{
			"page": map[string]interface{}{"type": "consent"},
		},
	)
	if got != "add_phone" {
		t.Fatalf("extractWorkspaceBlockerPageType() = %q, want %q", got, "add_phone")
	}

	got = extractWorkspaceBlockerPageType(
		map[string]interface{}{
			"page": map[string]interface{}{"type": "registration_disallowed"},
		},
		nil,
	)
	if got != "registration_disallowed" {
		t.Fatalf("extractWorkspaceBlockerPageType() = %q, want %q", got, "registration_disallowed")
	}

	if got := extractWorkspaceBlockerPageType(
		map[string]interface{}{"page": map[string]interface{}{"type": "consent"}},
		nil,
	); got != "" {
		t.Fatalf("extractWorkspaceBlockerPageType() = %q, want empty string when no blocker exists", got)
	}
}
