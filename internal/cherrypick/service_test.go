package cherrypick

import (
	"log/slog"
	"strings"
	"testing"

	"goauthentik.io/cherry-pick-svc/internal/config"
)

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
