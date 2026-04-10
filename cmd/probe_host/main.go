package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	executorruntime "github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"gopkg.in/yaml.v3"
)

const (
	defaultConfigPath = "config.yaml"
	defaultProbePath  = "probe.yaml"
	defaultPrompt     = "ping"
	defaultTimeoutSec = 30
	defaultUserAgent  = "cli-proxy-probe-host/1.0"
)

type probeTarget struct {
	label           string
	provider        string
	baseURL         string
	proxyURL        string
	apiKey          string
	headers         map[string]string
	model           string
	openAICompatRef string
}

type probeResult struct {
	status string
	body   []byte
}

type probeFileConfig struct {
	Prompt    string            `yaml:"prompt"`
	UserAgent string            `yaml:"user-agent"`
	Timeout   int               `yaml:"timeout"`
	Headers   map[string]string `yaml:"headers"`
}

type probeSettings struct {
	Prompt    string
	UserAgent string
	Timeout   time.Duration
	Headers   map[string]string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("probe_host", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var configPath string
	var probePath string
	var host string
	var timeoutSec int

	fs.StringVar(&configPath, "config", defaultConfigPath, "config file path")
	fs.StringVar(&probePath, "prob-config", defaultProbePath, "probe request config file path (deprecated alias)")
	fs.StringVar(&probePath, "probe-config", defaultProbePath, "probe request config file path")
	fs.StringVar(&host, "host", "", "host or URL to match against config base-url")
	fs.IntVar(&timeoutSec, "timeout", 0, "override per-request timeout in seconds")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s [-config path] [-probe-config path] -host host\n", fs.Name())
		fmt.Fprintf(stderr, "   or: %s [-config path] [-probe-config path] host\n", fs.Name())
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(host) == "" && fs.NArg() > 0 {
		host = fs.Arg(0)
	}
	if strings.TrimSpace(host) == "" {
		return errors.New("host is required")
	}

	probConfigExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "prob-config" || f.Name == "probe-config" {
			probConfigExplicit = true
		}
	})

	settings, err := loadProbeSettings(probePath, !probConfigExplicit && probePath == defaultProbePath)
	if err != nil {
		return err
	}
	if timeoutSec > 0 {
		settings.Timeout = time.Duration(timeoutSec) * time.Second
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config %q: %w", configPath, err)
	}

	targets := collectTargets(cfg, host)
	if len(targets) == 0 {
		return fmt.Errorf("no config entries matched host %q", host)
	}

	failures := 0
	for i, target := range targets {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		result, err := probe(context.Background(), cfg, settings, target)
		printResult(stdout, target, result, err)
		if err != nil {
			failures++
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d/%d requests failed", failures, len(targets))
	}
	return nil
}

func loadProbeSettings(path string, optional bool) (probeSettings, error) {
	settings := defaultProbeSettings()
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return settings, nil
	}

	data, err := os.ReadFile(trimmedPath)
	if err != nil {
		if optional && os.IsNotExist(err) {
			return settings, nil
		}
		return settings, fmt.Errorf("load probe config %q: %w", trimmedPath, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return settings, nil
	}

	var raw probeFileConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return settings, fmt.Errorf("parse probe config %q: %w", trimmedPath, err)
	}
	if value := strings.TrimSpace(raw.Prompt); value != "" {
		settings.Prompt = value
	}
	if value := strings.TrimSpace(raw.UserAgent); value != "" {
		settings.UserAgent = value
	}
	if raw.Timeout > 0 {
		settings.Timeout = time.Duration(raw.Timeout) * time.Second
	}
	if len(raw.Headers) > 0 {
		settings.Headers = cloneHeaders(raw.Headers)
	}

	return settings, nil
}

func defaultProbeSettings() probeSettings {
	return probeSettings{
		Prompt:    defaultPrompt,
		UserAgent: defaultUserAgent,
		Timeout:   time.Duration(defaultTimeoutSec) * time.Second,
	}
}

func collectTargets(cfg *config.Config, rawHost string) []probeTarget {
	if cfg == nil {
		return nil
	}

	var targets []probeTarget

	for i, entry := range cfg.CodexKey {
		baseURL := strings.TrimRight(strings.TrimSpace(entry.BaseURL), "/")
		if baseURL == "" || !hostMatches(baseURL, rawHost) {
			continue
		}
		targets = append(targets, probeTarget{
			label:    fmt.Sprintf("codex-api-key[%d]", i),
			provider: "codex",
			baseURL:  baseURL,
			proxyURL: strings.TrimSpace(entry.ProxyURL),
			apiKey:   strings.TrimSpace(entry.APIKey),
			headers:  cloneHeaders(entry.Headers),
			model:    firstNamedModel(entry.Models),
		})
	}

	for i, entry := range cfg.ClaudeKey {
		baseURL := strings.TrimRight(strings.TrimSpace(entry.BaseURL), "/")
		if baseURL == "" || !hostMatches(baseURL, rawHost) {
			continue
		}
		targets = append(targets, probeTarget{
			label:    fmt.Sprintf("claude-api-key[%d]", i),
			provider: "claude",
			baseURL:  baseURL,
			proxyURL: strings.TrimSpace(entry.ProxyURL),
			apiKey:   strings.TrimSpace(entry.APIKey),
			headers:  cloneHeaders(entry.Headers),
			model:    firstNamedModel(entry.Models),
		})
	}

	for i, entry := range cfg.GeminiKey {
		baseURL := strings.TrimRight(strings.TrimSpace(entry.BaseURL), "/")
		if baseURL == "" || !hostMatches(baseURL, rawHost) {
			continue
		}
		targets = append(targets, probeTarget{
			label:    fmt.Sprintf("gemini-api-key[%d]", i),
			provider: "gemini",
			baseURL:  baseURL,
			proxyURL: strings.TrimSpace(entry.ProxyURL),
			apiKey:   strings.TrimSpace(entry.APIKey),
			headers:  cloneHeaders(entry.Headers),
			model:    firstNamedModel(entry.Models),
		})
	}

	for i, entry := range cfg.VertexCompatAPIKey {
		baseURL := strings.TrimRight(strings.TrimSpace(entry.BaseURL), "/")
		if baseURL == "" || !hostMatches(baseURL, rawHost) {
			continue
		}
		targets = append(targets, probeTarget{
			label:    fmt.Sprintf("vertex-api-key[%d]", i),
			provider: "vertex",
			baseURL:  baseURL,
			proxyURL: strings.TrimSpace(entry.ProxyURL),
			apiKey:   strings.TrimSpace(entry.APIKey),
			headers:  cloneHeaders(entry.Headers),
			model:    firstNamedModel(entry.Models),
		})
	}

	for i, entry := range cfg.OpenAICompatibility {
		baseURL := strings.TrimRight(strings.TrimSpace(entry.BaseURL), "/")
		if baseURL == "" || !hostMatches(baseURL, rawHost) {
			continue
		}
		model := firstNamedModel(entry.Models)
		if len(entry.APIKeyEntries) == 0 {
			targets = append(targets, probeTarget{
				label:           fmt.Sprintf("openai-compatibility[%d]", i),
				provider:        "openai-compat",
				baseURL:         baseURL,
				headers:         cloneHeaders(entry.Headers),
				model:           model,
				openAICompatRef: strings.TrimSpace(entry.Name),
			})
			continue
		}
		for j, apiKeyEntry := range entry.APIKeyEntries {
			targets = append(targets, probeTarget{
				label:           fmt.Sprintf("openai-compatibility[%d].api-key-entries[%d]", i, j),
				provider:        "openai-compat",
				baseURL:         baseURL,
				proxyURL:        strings.TrimSpace(apiKeyEntry.ProxyURL),
				apiKey:          strings.TrimSpace(apiKeyEntry.APIKey),
				headers:         cloneHeaders(entry.Headers),
				model:           model,
				openAICompatRef: strings.TrimSpace(entry.Name),
			})
		}
	}

	return targets
}

func probe(ctx context.Context, cfg *config.Config, settings probeSettings, target probeTarget) (probeResult, error) {
	exec, auth, req, opts, err := buildProbeExecution(cfg, settings, target)
	if err != nil {
		return probeResult{}, err
	}

	execCtx, cancel := context.WithTimeout(ctx, settings.Timeout)
	defer cancel()

	resp, err := exec.Execute(execCtx, auth, req, opts)
	if err != nil {
		status := "error"
		var statusErr cliproxyexecutor.StatusError
		if errors.As(err, &statusErr) {
			status = fmt.Sprintf("error (%d)", statusErr.StatusCode())
		}
		return probeResult{status: status}, err
	}

	return probeResult{
		status: "ok",
		body:   resp.Payload,
	}, nil
}

func buildProbeExecution(cfg *config.Config, settings probeSettings, target probeTarget) (coreauth.ProviderExecutor, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options, error) {
	auth := buildProbeAuth(settings, target)

	switch target.provider {
	case "codex":
		if strings.TrimSpace(target.model) == "" {
			return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, fmt.Errorf("%s: no model configured", target.label)
		}
		payload, err := json.Marshal(map[string]any{
			"model":        target.model,
			"instructions": "",
			"input":        settings.Prompt,
		})
		if err != nil {
			return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, err
		}
		return executorruntime.NewCodexExecutor(cfg), auth, cliproxyexecutor.Request{
				Model:   target.model,
				Payload: payload,
			}, cliproxyexecutor.Options{
				SourceFormat: sdktranslator.FromString("openai-response"),
				Stream:       false,
			}, nil
	case "claude":
		if strings.TrimSpace(target.model) == "" {
			return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, fmt.Errorf("%s: no model configured", target.label)
		}
		payload, err := json.Marshal(map[string]any{
			"model":      target.model,
			"max_tokens": 32,
			"messages": []map[string]any{
				{
					"role": "user",
					"content": []map[string]string{
						{"type": "text", "text": settings.Prompt},
					},
				},
			},
		})
		if err != nil {
			return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, err
		}
		return executorruntime.NewClaudeExecutor(cfg), auth, cliproxyexecutor.Request{
				Model:   target.model,
				Payload: payload,
			}, cliproxyexecutor.Options{
				SourceFormat: sdktranslator.FromString("claude"),
				Stream:       false,
			}, nil
	case "gemini":
		if strings.TrimSpace(target.model) == "" {
			return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, fmt.Errorf("%s: no model configured", target.label)
		}
		payload, err := json.Marshal(map[string]any{
			"model": target.model,
			"contents": []map[string]any{
				{
					"role": "user",
					"parts": []map[string]string{
						{"text": settings.Prompt},
					},
				},
			},
		})
		if err != nil {
			return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, err
		}
		return executorruntime.NewGeminiExecutor(cfg), auth, cliproxyexecutor.Request{
				Model:   target.model,
				Payload: payload,
			}, cliproxyexecutor.Options{
				SourceFormat: sdktranslator.FromString("gemini"),
				Stream:       false,
			}, nil
	case "vertex":
		if strings.TrimSpace(target.model) == "" {
			return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, fmt.Errorf("%s: no model configured", target.label)
		}
		payload, err := json.Marshal(map[string]any{
			"model": target.model,
			"contents": []map[string]any{
				{
					"role": "user",
					"parts": []map[string]string{
						{"text": settings.Prompt},
					},
				},
			},
		})
		if err != nil {
			return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, err
		}
		return executorruntime.NewGeminiVertexExecutor(cfg), auth, cliproxyexecutor.Request{
				Model:   target.model,
				Payload: payload,
			}, cliproxyexecutor.Options{
				SourceFormat: sdktranslator.FromString("gemini"),
				Stream:       false,
			}, nil
	case "openai-compat":
		if strings.TrimSpace(target.model) == "" {
			return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, fmt.Errorf("%s: no model configured", target.label)
		}
		payload, err := json.Marshal(map[string]any{
			"model": target.model,
			"messages": []map[string]string{
				{"role": "user", "content": settings.Prompt},
			},
			"stream": false,
		})
		if err != nil {
			return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, err
		}
		return executorruntime.NewOpenAICompatExecutor("openai-compatibility", cfg), auth, cliproxyexecutor.Request{
				Model:   target.model,
				Payload: payload,
			}, cliproxyexecutor.Options{
				SourceFormat: sdktranslator.FromString("openai"),
				Stream:       false,
			}, nil
	default:
		return nil, nil, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, fmt.Errorf("unsupported provider %q", target.provider)
	}
}

func buildProbeAuth(settings probeSettings, target probeTarget) *coreauth.Auth {
	attrs := map[string]string{
		"base_url": target.baseURL,
		"api_key":  target.apiKey,
	}
	mergeHeadersIntoAttrs(attrs, settings.Headers)
	if settings.UserAgent != "" {
		attrs["header:User-Agent"] = settings.UserAgent
	}
	mergeHeadersIntoAttrs(attrs, target.headers)

	return &coreauth.Auth{
		Provider:   target.provider,
		Label:      target.label,
		ProxyURL:   target.proxyURL,
		Attributes: attrs,
	}
}

func printResult(w io.Writer, target probeTarget, result probeResult, err error) {
	fmt.Fprintf(w, "== %s ==\n", target.label)
	fmt.Fprintf(w, "provider: %s\n", target.provider)
	if target.openAICompatRef != "" {
		fmt.Fprintf(w, "name: %s\n", target.openAICompatRef)
	}
	fmt.Fprintf(w, "base-url: %s\n", target.baseURL)
	if target.proxyURL != "" {
		fmt.Fprintf(w, "proxy-url: %s\n", target.proxyURL)
	}
	if target.model != "" {
		fmt.Fprintf(w, "model: %s\n", target.model)
	}
	if err != nil {
		if result.status != "" {
			fmt.Fprintf(w, "status: %s\n", result.status)
		}
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	fmt.Fprintf(w, "status: %s\n", result.status)
	fmt.Fprintln(w, "response:")
	fmt.Fprintln(w, formatBody(result.body))
}

func formatBody(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return "<empty>"
	}

	var pretty bytes.Buffer
	if json.Indent(&pretty, trimmed, "", "  ") == nil {
		return pretty.String()
	}
	return string(trimmed)
}

func cloneHeaders(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func hostMatches(rawBaseURL, rawHost string) bool {
	baseHost, baseHostPort := normalizeURLHost(rawBaseURL)
	targetHost, targetHostPort := extractHostFromInput(rawHost)
	if targetHost == "" {
		return false
	}
	if strings.EqualFold(baseHost, targetHost) {
		return true
	}
	if targetHostPort != "" && strings.EqualFold(baseHostPort, targetHostPort) {
		return true
	}
	return false
}

func normalizeURLHost(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	return strings.ToLower(parsed.Hostname()), strings.ToLower(parsed.Host)
}

// extractHostFromInput accepts a plain hostname, host:port, or a full URL
// and returns (hostname, host:port). When the input is a URL the path and
// other components are ignored so that e.g. "https://api.example.com/v1"
// and "api.example.com" both resolve to the same host for comparison.
func extractHostFromInput(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	// Full URL – has a scheme.
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", ""
		}
		return strings.ToLower(parsed.Hostname()), strings.ToLower(parsed.Host)
	}

	// Looks like host:port or plain host – prepend a scheme so url.Parse
	// can split host from port correctly.
	parsed, err := url.Parse("https://" + raw)
	if err != nil {
		return "", ""
	}
	hostname := strings.ToLower(parsed.Hostname())
	hostPort := strings.ToLower(parsed.Host)

	// url.Parse may put a path-only value into Path instead of Host when
	// there are no slashes; fall back to the raw trimmed value in that case.
	if hostname == "" {
		trimmed := strings.ToLower(strings.Trim(raw, "/"))
		return trimmed, trimmed
	}
	return hostname, hostPort
}

func mergeHeadersIntoAttrs(attrs map[string]string, headers map[string]string) {
	for key, value := range headers {
		if attrs == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		attrs["header:"+key] = value
	}
}

type namedModel interface {
	GetName() string
	GetAlias() string
}

func firstNamedModel[T namedModel](models []T) string {
	for _, model := range models {
		if name := strings.TrimSpace(model.GetName()); name != "" {
			return name
		}
		if alias := strings.TrimSpace(model.GetAlias()); alias != "" {
			return alias
		}
	}
	return ""
}
