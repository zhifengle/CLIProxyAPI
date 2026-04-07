package config

import "testing"

func TestSanitizeGlobalModelMappings(t *testing.T) {
	cfg := &Config{
		GlobalModelMappings: []GlobalModelMapping{
			{From: " gpt-5-nano ", To: " gpt-5.4-mini "},
			{From: "^claude-opus-4-5.*$", To: " claude-sonnet-4-5-20250929 ", Regex: true},
			{From: "", To: "missing-from"},
			{From: "missing-to", To: ""},
		},
	}

	cfg.SanitizeGlobalModelMappings()

	if len(cfg.GlobalModelMappings) != 2 {
		t.Fatalf("expected 2 sanitized global model mappings, got %d", len(cfg.GlobalModelMappings))
	}

	if got := cfg.GlobalModelMappings[0]; got.From != "gpt-5-nano" || got.To != "gpt-5.4-mini" || got.Regex {
		t.Fatalf("unexpected first mapping: %+v", got)
	}
	if got := cfg.GlobalModelMappings[1]; got.From != "^claude-opus-4-5.*$" || got.To != "claude-sonnet-4-5-20250929" || !got.Regex {
		t.Fatalf("unexpected second mapping: %+v", got)
	}
}
