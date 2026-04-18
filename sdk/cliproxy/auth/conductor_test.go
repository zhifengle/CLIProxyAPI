package auth

import (
	"testing"
)

func TestAuthBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth *Auth
		want string
	}{
		{
			name: "nil auth returns empty",
			auth: nil,
			want: "",
		},
		{
			name: "nil attributes and metadata returns empty",
			auth: &Auth{},
			want: "",
		},
		{
			name: "attributes base_url wins",
			auth: &Auth{
				Attributes: map[string]string{"base_url": "https://api.example.com"},
				Metadata:   map[string]any{"base_url": "https://ignored.com"},
			},
			want: "https://api.example.com",
		},
		{
			name: "attributes base-url hyphen variant",
			auth: &Auth{
				Attributes: map[string]string{"base-url": "https://api.example.com"},
			},
			want: "https://api.example.com",
		},
		{
			name: "attributes base_url preferred over base-url",
			auth: &Auth{
				Attributes: map[string]string{
					"base_url": "https://preferred.com",
					"base-url": "https://fallback.com",
				},
			},
			want: "https://preferred.com",
		},
		{
			name: "metadata base_url fallback",
			auth: &Auth{
				Metadata: map[string]any{"base_url": "https://meta.example.com"},
			},
			want: "https://meta.example.com",
		},
		{
			name: "metadata base-url hyphen variant",
			auth: &Auth{
				Metadata: map[string]any{"base-url": "https://meta.example.com"},
			},
			want: "https://meta.example.com",
		},
		{
			name: "whitespace trimmed",
			auth: &Auth{
				Attributes: map[string]string{"base_url": "  https://api.example.com  "},
			},
			want: "https://api.example.com",
		},
		{
			name: "empty string attribute skipped",
			auth: &Auth{
				Attributes: map[string]string{"base_url": "  "},
				Metadata:   map[string]any{"base_url": "https://fallback.com"},
			},
			want: "https://fallback.com",
		},
		{
			name: "non-string metadata value skipped",
			auth: &Auth{
				Metadata: map[string]any{"base_url": 42},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := authBaseURL(tt.auth); got != tt.want {
				t.Fatalf("authBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
