package main

import (
	"path/filepath"
	"testing"
)

func TestBrowserFlowConfigNormalizedDefaults(t *testing.T) {
	cfg := (&BrowserFlowConfig{}).normalized()
	if cfg.Engine != "camoufox" {
		t.Fatalf("normalized engine = %q, want %q", cfg.Engine, "camoufox")
	}
	if cfg.PythonBin != "python" {
		t.Fatalf("normalized python bin = %q, want %q", cfg.PythonBin, "python")
	}
	if filepath.Base(cfg.ScriptPath) != "camoufox_worker.py" {
		t.Fatalf("normalized script path = %q, want camoufox_worker.py basename", cfg.ScriptPath)
	}
}

func TestInvokeBrowserWorkerHandshake(t *testing.T) {
	resp, err := invokeBrowserWorker(nil, BrowserWorkerRequest{Action: "handshake"})
	if err != nil {
		t.Fatalf("invokeBrowserWorker() unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("invokeBrowserWorker() ok = false, error=%q", resp.Error)
	}
	if resp.Engine != "camoufox" {
		t.Fatalf("invokeBrowserWorker() engine = %q, want %q", resp.Engine, "camoufox")
	}
	if resp.Status == "" {
		t.Fatal("invokeBrowserWorker() status is empty")
	}
}

func TestInvokeBrowserWorkerStartFlowRequiresAuthURL(t *testing.T) {
	resp, err := invokeBrowserWorker(nil, BrowserWorkerRequest{Action: "start_flow"})
	if err != nil {
		t.Fatalf("invokeBrowserWorker() unexpected error: %v", err)
	}
	if resp.OK {
		t.Fatal("invokeBrowserWorker() ok = true, want false when auth_url is missing")
	}
	if resp.Status != "invalid_request" {
		t.Fatalf("invokeBrowserWorker() status = %q, want %q", resp.Status, "invalid_request")
	}
}

func TestInvokeBrowserWorkerSubmitOTPRequiresSessionID(t *testing.T) {
	resp, err := invokeBrowserWorker(nil, BrowserWorkerRequest{
		Action: "submit_otp",
		OTP:    "123456",
	})
	if err != nil {
		t.Fatalf("invokeBrowserWorker() unexpected error: %v", err)
	}
	if resp.OK {
		t.Fatal("invokeBrowserWorker() ok = true, want false when session_id is missing")
	}
	if resp.Status != "invalid_request" {
		t.Fatalf("invokeBrowserWorker() status = %q, want %q", resp.Status, "invalid_request")
	}
}

func TestInvokeBrowserWorkerSubmitOTPRequiresOTP(t *testing.T) {
	resp, err := invokeBrowserWorker(nil, BrowserWorkerRequest{
		Action:    "submit_otp",
		SessionID: "test-session",
	})
	if err != nil {
		t.Fatalf("invokeBrowserWorker() unexpected error: %v", err)
	}
	if resp.OK {
		t.Fatal("invokeBrowserWorker() ok = true, want false when otp is missing")
	}
	if resp.Status != "invalid_request" {
		t.Fatalf("invokeBrowserWorker() status = %q, want %q", resp.Status, "invalid_request")
	}
}
