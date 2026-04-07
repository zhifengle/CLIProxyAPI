package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestRunPrintsMultipleMatchesForSameHost(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		messages, ok := payload["messages"].([]any)
		if !ok || len(messages) == 0 {
			t.Fatalf("messages missing from payload: %#v", payload)
		}
		firstMessage, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatalf("first message type = %T", messages[0])
		}
		if got := firstMessage["content"]; got != "hello from prob" {
			t.Fatalf("prompt = %#v, want %q", got, "hello from prob")
		}
		if got := r.Header.Get("User-Agent"); got != "probe-agent/2.0" {
			t.Fatalf("user-agent = %q, want %q", got, "probe-agent/2.0")
		}
		if got := r.Header.Get("X-Probe-Header"); got != "enabled" {
			t.Fatalf("x-probe-header = %q, want %q", got, "enabled")
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/team-one/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"provider": "one",
				"auth":     r.Header.Get("Authorization"),
				"model":    payload["model"],
				"prompt":   firstMessage["content"],
				"ua":       r.Header.Get("User-Agent"),
			})
		case "/team-two/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"provider": "two",
				"auth":     r.Header.Get("Authorization"),
				"model":    payload["model"],
				"prompt":   firstMessage["content"],
				"ua":       r.Header.Get("User-Agent"),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	configBody := fmt.Sprintf(`proxy-url: "direct"
openai-compatibility:
  - name: "one"
    base-url: "%s/team-one"
    api-key-entries:
      - api-key: "key-one"
        proxy-url: "direct"
    models:
      - name: "model-one"
  - name: "two"
    base-url: "%s/team-two"
    api-key-entries:
      - api-key: "key-two"
        proxy-url: "direct"
    models:
      - name: "model-two"
`, server.URL, server.URL)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	probePath := filepath.Join(tempDir, "probe.yaml")
	probeBody := `prompt: "hello from prob"
user-agent: "probe-agent/2.0"
timeout: 2
headers:
  X-Probe-Header: "enabled"
`
	if err := os.WriteFile(probePath, []byte(probeBody), 0o600); err != nil {
		t.Fatalf("write probe config: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = run([]string{"-config", configPath, "-probe-config", probePath, "-host", parsedURL.Hostname()}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}

	got := stdout.String()
	if strings.Count(got, "status: ok") != 2 {
		t.Fatalf("status count = %d, want 2\noutput:\n%s", strings.Count(got, "status: ok"), got)
	}
	if !strings.Contains(got, "== openai-compatibility[0].api-key-entries[0] ==") {
		t.Fatalf("missing first target label\noutput:\n%s", got)
	}
	if !strings.Contains(got, "== openai-compatibility[1].api-key-entries[0] ==") {
		t.Fatalf("missing second target label\noutput:\n%s", got)
	}
	if !strings.Contains(got, `"provider": "one"`) {
		t.Fatalf("missing provider one response\noutput:\n%s", got)
	}
	if !strings.Contains(got, `"provider": "two"`) {
		t.Fatalf("missing provider two response\noutput:\n%s", got)
	}
	if !strings.Contains(got, `"auth": "Bearer key-one"`) {
		t.Fatalf("missing first auth header\noutput:\n%s", got)
	}
	if !strings.Contains(got, `"auth": "Bearer key-two"`) {
		t.Fatalf("missing second auth header\noutput:\n%s", got)
	}
	if strings.Count(got, `"prompt": "hello from prob"`) != 2 {
		t.Fatalf("prompt count = %d, want 2\noutput:\n%s", strings.Count(got, `"prompt": "hello from prob"`), got)
	}
	if strings.Count(got, `"ua": "probe-agent/2.0"`) != 2 {
		t.Fatalf("user-agent count = %d, want 2\noutput:\n%s", strings.Count(got, `"ua": "probe-agent/2.0"`), got)
	}
}

func TestCollectTargetsSkipsEntriesWithoutBaseURL(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		CodexKey: []config.CodexKey{
			{APIKey: "test-key"},
		},
	}

	targets := collectTargets(cfg, "example.com")
	if len(targets) != 0 {
		t.Fatalf("targets len = %d, want 0", len(targets))
	}
}

func TestCollectTargetsUsesFirstModelAndIgnoresExcludedModels(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		CodexKey: []config.CodexKey{
			{
				APIKey:         "test-key",
				BaseURL:        "https://example.com/v1",
				ExcludedModels: []string{"gpt-5.4"},
				Models: []config.CodexModel{
					{Name: "gpt-5.4"},
					{Name: "gpt-5.4-mini"},
				},
			},
		},
	}

	targets := collectTargets(cfg, "example.com")
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(targets))
	}
	if got := targets[0].model; got != "gpt-5.4" {
		t.Fatalf("model = %q, want %q", got, "gpt-5.4")
	}
}

func TestLoadProbeSettingsUsesDefaultsWhenMissing(t *testing.T) {
	t.Parallel()

	settings, err := loadProbeSettings(filepath.Join(t.TempDir(), "probe.yaml"), true)
	if err != nil {
		t.Fatalf("loadProbeSettings returned error: %v", err)
	}
	if settings.Prompt != defaultPrompt {
		t.Fatalf("prompt = %q, want %q", settings.Prompt, defaultPrompt)
	}
	if settings.UserAgent != defaultUserAgent {
		t.Fatalf("user-agent = %q, want %q", settings.UserAgent, defaultUserAgent)
	}
	if settings.Timeout != time.Duration(defaultTimeoutSec)*time.Second {
		t.Fatalf("timeout = %s, want %ds", settings.Timeout, defaultTimeoutSec)
	}
}
