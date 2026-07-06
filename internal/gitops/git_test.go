package gitops

import "testing"

func TestRedactToken(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no token",
			input: "nothing to redact here",
			want:  "nothing to redact here",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "single token",
			input: "https://x-access-token:ghs_abc123@github.com/owner/repo.git",
			want:  "https://x-access-token:***@github.com/owner/repo.git",
		},
		{
			name:  "token in git error output",
			input: "fatal: could not read 'https://x-access-token:secret@github.com/org/repo.git': not found",
			want:  "fatal: could not read 'https://x-access-token:***@github.com/org/repo.git': not found",
		},
		{
			name:  "no at-sign after token prefix",
			input: "https://x-access-token:no-at-sign-follows",
			want:  "https://x-access-token:no-at-sign-follows",
		},
		{
			name:  "multiple tokens",
			input: "cloned https://x-access-token:tok1@github.com and https://x-access-token:tok2@github.com",
			want:  "cloned https://x-access-token:***@github.com and https://x-access-token:***@github.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactToken(tt.input)
			if got != tt.want {
				t.Errorf("redactToken(%q)\n  got  %q\n  want %q", tt.input, got, tt.want)
			}
		})
	}
}
