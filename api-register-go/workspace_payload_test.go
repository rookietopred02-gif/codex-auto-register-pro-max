package main

import (
	"encoding/base64"
	"testing"
)

func TestDecodeSessionCookiePayload(t *testing.T) {
	payload := []byte(`{"workspaces":[{"id":"ws_123"}]}`)

	tests := []struct {
		name    string
		segment string
		wantErr bool
	}{
		{
			name:    "raw-url",
			segment: base64.RawURLEncoding.EncodeToString(payload),
		},
		{
			name:    "url",
			segment: base64.URLEncoding.EncodeToString(payload),
		},
		{
			name:    "raw-std",
			segment: base64.RawStdEncoding.EncodeToString(payload),
		},
		{
			name:    "std",
			segment: base64.StdEncoding.EncodeToString(payload),
		},
		{
			name:    "invalid",
			segment: "not-base64!!!",
			wantErr: true,
		},
		{
			name:    "empty",
			segment: "   ",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeSessionCookiePayload(tc.segment)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != string(payload) {
				t.Fatalf("decoded payload mismatch: got %q want %q", string(got), string(payload))
			}
		})
	}
}
