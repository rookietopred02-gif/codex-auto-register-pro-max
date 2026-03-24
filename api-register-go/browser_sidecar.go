package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type BrowserFlowConfig struct {
	Enabled    bool   `json:"enabled,omitempty"`
	Engine     string `json:"engine,omitempty"`
	PythonBin  string `json:"python_bin,omitempty"`
	ScriptPath string `json:"script_path,omitempty"`
	Headless   *bool  `json:"headless,omitempty"`
}

type BrowserWorkerRequest struct {
	Action     string                 `json:"action"`
	Engine     string                 `json:"engine,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	AuthURL    string                 `json:"auth_url,omitempty"`
	CurrentURL string                 `json:"current_url,omitempty"`
	Email      string                 `json:"email,omitempty"`
	Password   string                 `json:"password,omitempty"`
	Proxy      string                 `json:"proxy,omitempty"`
	Mode       string                 `json:"mode,omitempty"`
	OTP        string                 `json:"otp,omitempty"`
	Headless   *bool                  `json:"headless,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type BrowserWorkerResponse struct {
	OK              bool                   `json:"ok"`
	Ready           bool                   `json:"ready,omitempty"`
	Engine          string                 `json:"engine,omitempty"`
	SessionID       string                 `json:"session_id,omitempty"`
	Status          string                 `json:"status,omitempty"`
	PageType        string                 `json:"page_type,omitempty"`
	BlockerPageType string                 `json:"blocker_page_type,omitempty"`
	CurrentURL      string                 `json:"current_url,omitempty"`
	CallbackURL     string                 `json:"callback_url,omitempty"`
	Error           string                 `json:"error,omitempty"`
	Debug           map[string]interface{} `json:"debug,omitempty"`
}

func defaultBrowserWorkerScriptPath() string {
	wd, err := os.Getwd()
	if err != nil {
		return "camoufox_worker.py"
	}
	return filepath.Join(wd, "camoufox_worker.py")
}

func (cfg *BrowserFlowConfig) normalized() BrowserFlowConfig {
	out := BrowserFlowConfig{}
	if cfg != nil {
		out = *cfg
	}
	if strings.TrimSpace(out.Engine) == "" {
		out.Engine = "camoufox"
	}
	if strings.TrimSpace(out.PythonBin) == "" {
		out.PythonBin = "python"
	}
	if strings.TrimSpace(out.ScriptPath) == "" {
		out.ScriptPath = defaultBrowserWorkerScriptPath()
	}
	return out
}

func invokeBrowserWorker(cfg *BrowserFlowConfig, req BrowserWorkerRequest) (*BrowserWorkerResponse, error) {
	normalized := cfg.normalized()
	if strings.TrimSpace(req.Action) == "" {
		return nil, fmt.Errorf("browser worker action is required")
	}
	if strings.TrimSpace(req.Engine) == "" {
		req.Engine = normalized.Engine
	}
	if req.Headless == nil {
		req.Headless = normalized.Headless
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal browser worker request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, normalized.PythonBin, normalized.ScriptPath)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("browser worker timeout after %s", 90*time.Second)
		}
		return nil, fmt.Errorf("browser worker exec failed: %w; stderr=%s", err, strings.TrimSpace(stderr.String()))
	}

	var resp BrowserWorkerResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("parse browser worker response: %w; stdout=%s; stderr=%s", err, strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()))
	}
	return &resp, nil
}
