package auth

import (
	"context"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func TestApplyGlobalModelMapping_SuffixPreservation(t *testing.T) {
	t.Parallel()

	mgr := NewManager(nil, nil, nil)
	mgr.SetConfig(&internalconfig.Config{
		GlobalModelMappings: []internalconfig.GlobalModelMapping{
			{From: "gpt-5-nano", To: "gpt-5.4-mini"},
			{From: "^claude-opus-4-5.*$", To: "claude-sonnet-4-5-20250929", Regex: true},
			{From: "gpt-5-fast", To: "gpt-5.4-mini(low)"},
		},
	})

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "exact preserves numeric suffix", input: "gpt-5-nano(8192)", want: "gpt-5.4-mini(8192)"},
		{name: "exact preserves level suffix", input: "gpt-5-nano(high)", want: "gpt-5.4-mini(high)"},
		{name: "regex preserves suffix", input: "claude-opus-4-5-20251101(high)", want: "claude-sonnet-4-5-20250929(high)"},
		{name: "config suffix wins", input: "gpt-5-fast(high)", want: "gpt-5.4-mini(low)"},
		{name: "no mapping passthrough", input: "unknown-model", want: "unknown-model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mgr.applyGlobalModelMapping(tt.input); got != tt.want {
				t.Fatalf("applyGlobalModelMapping(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestManagerExecute_GlobalModelMappingPrecedesAPIKeyAlias(t *testing.T) {
	const provider = "codex"

	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{
		GlobalModelMappings: []internalconfig.GlobalModelMapping{
			{From: "gpt-5-nano", To: "codex-latest"},
		},
		CodexKey: []internalconfig.CodexKey{
			{
				APIKey:  "k",
				BaseURL: "https://example.com",
				Models: []internalconfig.CodexModel{
					{Name: "wrong-model", Alias: "gpt-5-nano"},
					{Name: "gpt-5.4-mini", Alias: "codex-latest"},
				},
			},
		},
	})

	executor := &aliasRoutingExecutor{id: provider}
	manager.RegisterExecutor(executor)

	auth := &Auth{
		ID:         "global-map-auth",
		Provider:   provider,
		Status:     StatusActive,
		Attributes: map[string]string{"api_key": "k"},
	}
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, provider, []*registry.ModelInfo{{ID: "codex-latest"}})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})
	manager.RefreshSchedulerEntry(auth.ID)

	resp, errExecute := manager.Execute(context.Background(), []string{provider}, cliproxyexecutor.Request{Model: "gpt-5-nano"}, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("execute error = %v, want success", errExecute)
	}
	if string(resp.Payload) != "gpt-5.4-mini" {
		t.Fatalf("execute payload = %q, want %q", string(resp.Payload), "gpt-5.4-mini")
	}

	gotModels := executor.ExecuteModels()
	if len(gotModels) != 1 {
		t.Fatalf("execute models len = %d, want 1", len(gotModels))
	}
	if gotModels[0] != "gpt-5.4-mini" {
		t.Fatalf("execute model = %q, want %q", gotModels[0], "gpt-5.4-mini")
	}
}
