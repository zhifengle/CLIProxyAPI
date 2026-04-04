package logging

import (
	"strings"
	"testing"
)

func TestFormatLogContentIncludesRequestSummaryFromHTTPUpstream(t *testing.T) {
	logger := &FileRequestLogger{}
	content := logger.formatLogContent(
		"/v1/chat/completions",
		"POST",
		map[string][]string{
			"User-Agent":          {"OpenAI/Python 1.50.0"},
			"X-Stainless-Package": {"openai"},
			"X-Stainless-Lang":    {"python"},
		},
		[]byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`),
		nil,
		[]byte("=== API REQUEST 1 ===\nTimestamp: 2026-04-04T12:00:00Z\nUpstream URL: https://api.openai.com/v1/chat/completions\nHTTP Method: POST\n\nHeaders:\nContent-Type: application/json\n\nBody:\n{\"model\":\"gpt-5.4-mini\"}\n\n"),
		nil,
		nil,
		[]byte(`{"id":"resp_1"}`),
		200,
		map[string][]string{"Content-Type": {"application/json"}},
		nil,
	)

	for _, want := range []string{
		"Client: user-agent=OpenAI/Python 1.50.0, stainless-package=openai, stainless-lang=python",
		"Requested Model: gpt-5.4",
		"Upstream Model: gpt-5.4-mini",
		"Upstream URL: https://api.openai.com/v1/chat/completions",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("log content missing %q\n%s", want, content)
		}
	}
}

func TestFormatLogContentIncludesRequestSummaryFromWebsocketUpstream(t *testing.T) {
	logger := &FileRequestLogger{}
	content := logger.formatLogContent(
		"/v1/responses",
		"POST",
		map[string][]string{"User-Agent": {"Codex/1.2.3"}},
		[]byte(`{"model":"gpt-5.4-codex","input":[{"role":"user","content":"hi"}]}`),
		nil,
		nil,
		nil,
		[]byte("Timestamp: 2026-04-04T12:00:00Z\nEvent: api.websocket.request\nUpstream URL: wss://chatgpt.com/backend-api/codex/responses\nHeaders:\n<none>\n\nBody:\n{\"model\":\"gpt-5.4-codex\"}\n"),
		nil,
		101,
		nil,
		nil,
	)

	for _, want := range []string{
		"Client: user-agent=Codex/1.2.3",
		"Requested Model: gpt-5.4-codex",
		"Upstream Model: gpt-5.4-codex",
		"Upstream URL: wss://chatgpt.com/backend-api/codex/responses",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("log content missing %q\n%s", want, content)
		}
	}
}
