package cherrypick

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/authentik-labs/mewp/internal/config"
)

func TestValidateTargetBranch(t *testing.T) {
	tests := []struct {
		branch  string
		wantErr bool
	}{
		{"main", false},
		{"v1.2", false},
		{"release/1.0", false},
		{"feature-branch", false},
		{"v1.2.3", false},
		{"release_candidate", false},
		// starts with dash — git option injection vector
		{"-dangerous", true},
		{"--flag", true},
		// characters outside the allowlist
		{"branch with spaces", true},
		{"branch;rm$HOME", true},
		{"branch`cmd`", true},
		// empty string
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			err := validateTargetBranch(tt.branch)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTargetBranch(%q) error = %v, wantErr = %v", tt.branch, err, tt.wantErr)
			}
		})
	}
}

func TestBuildPRBody(t *testing.T) {
	j := NewJob(&config.Config{}, slog.Default(), 1, "owner", "repo", 42, "Fix the bug", "author", "abc123def", "v1.0")

	t.Run("no conflicts", func(t *testing.T) {
		body := j.buildPRBody(false)
		for _, want := range []string{"#42", "`v1.0`", "@author", "abc123def"} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "conflict") {
			t.Errorf("conflict-free body should not mention conflicts:\n%s", body)
		}
	})

	t.Run("with conflicts", func(t *testing.T) {
		body := j.buildPRBody(true)
		for _, want := range []string{"#42", "`v1.0`", "@author", "abc123def", "conflict"} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q:\n%s", want, body)
			}
		}
	})
}
