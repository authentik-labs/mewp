package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"goauthentik.io/cherry-pick-svc/internal/config"
)

func signBody(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newTestServer(secret string) *Server {
	cfg := &config.Config{
		WebhookSecret: secret,
		AppID:         1,
		AppPrivateKey: []byte("placeholder"),
	}
	return NewServer(cfg, slog.Default())
}

func TestVerifySignature(t *testing.T) {
	const secret = "s3cr3t"
	const body = `{"action":"closed"}`
	validSig := signBody(secret, body)

	tests := []struct {
		name    string
		sig     string
		wantErr bool
	}{
		{"valid", validSig, false},
		{"missing header", "", true},
		{"too short (no hex)", "sha256=", true},
		{"bad prefix", "md5=" + validSig[7:], true},
		{"bad hex digits", "sha256=zzzzzzzz", true},
		{"wrong secret", signBody("other-secret", body), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString(body))
			if tt.sig != "" {
				req.Header.Set("X-Hub-Signature-256", tt.sig)
			}
			_, err := verifySignature(secret, req)
			if (err != nil) != tt.wantErr {
				t.Errorf("verifySignature() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestBackportLabelRe(t *testing.T) {
	tests := []struct {
		label  string
		branch string // empty means no match expected
	}{
		{"backport/v1.2", "v1.2"},
		{"backport/main", "main"},
		{"backport/release/1.0", "release/1.0"},
		{"bug", ""},
		{"backport", ""},
		{"backport/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			m := backportLabelRe.FindStringSubmatch(tt.label)
			if tt.branch == "" {
				if m != nil {
					t.Errorf("unexpected match for %q: %v", tt.label, m)
				}
				return
			}
			if m == nil {
				t.Fatalf("expected match for %q, got nil", tt.label)
			}
			if m[1] != tt.branch {
				t.Errorf("label %q: got branch %q, want %q", tt.label, m[1], tt.branch)
			}
		})
	}
}

func TestServeHTTP(t *testing.T) {
	const secret = "webhook-secret"

	makeReq := func(event, body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString(body))
		req.Header.Set("X-Hub-Signature-256", signBody(secret, body))
		if event != "" {
			req.Header.Set("X-GitHub-Event", event)
		}
		return req
	}

	tests := []struct {
		name       string
		req        *http.Request
		wantStatus int
	}{
		{
			name: "invalid signature",
			req: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{}"))
				req.Header.Set("X-Hub-Signature-256", "sha256=bad")
				req.Header.Set("X-GitHub-Event", "push")
				return req
			}(),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "unknown event",
			req:        makeReq("ping", `{}`),
			wantStatus: http.StatusOK,
		},
		{
			name:       "pull_request bad json",
			req:        makeReq("pull_request", `not-json`),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "issues bad json",
			req:        makeReq("issues", `not-json`),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "pull_request closed not merged",
			req: makeReq("pull_request", `{"action":"closed","pull_request":{"merged":false},"repository":{"name":"r","owner":{"login":"o"}},"installation":{"id":1}}`),
			wantStatus: http.StatusOK,
		},
		{
			name: "issues not labeled action",
			req: makeReq("issues", `{"action":"opened","issue":{"number":1,"pull_request":{"url":"u"}},"repository":{"name":"r","owner":{"login":"o"}},"installation":{"id":1}}`),
			wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(secret)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, tt.req)
			s.WaitAll()
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestDispatchPullRequestEvent_EarlyReturns(t *testing.T) {
	tests := []struct {
		name  string
		event *PullRequestEvent
	}{
		{
			name:  "closed not merged",
			event: &PullRequestEvent{Action: "closed", PullRequest: PullRequest{Merged: false, Labels: []Label{{Name: "backport/v1"}}}},
		},
		{
			name:  "closed merged no backport labels",
			event: &PullRequestEvent{Action: "closed", PullRequest: PullRequest{Merged: true, Labels: []Label{{Name: "bug"}}}},
		},
		{
			name:  "labeled not merged",
			event: &PullRequestEvent{Action: "labeled", Label: &Label{Name: "backport/v1"}, PullRequest: PullRequest{Merged: false}},
		},
		{
			name:  "labeled nil label field",
			event: &PullRequestEvent{Action: "labeled", Label: nil, PullRequest: PullRequest{Merged: true}},
		},
		{
			name:  "labeled non-backport label",
			event: &PullRequestEvent{Action: "labeled", Label: &Label{Name: "enhancement"}, PullRequest: PullRequest{Merged: true}},
		},
		{
			name:  "unrelated action",
			event: &PullRequestEvent{Action: "opened", PullRequest: PullRequest{Labels: []Label{{Name: "backport/v1"}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer("secret")
			s.dispatchPullRequestEvent(tt.event)
			s.WaitAll()
		})
	}
}

func TestDispatchIssueEvent_EarlyReturns(t *testing.T) {
	tests := []struct {
		name  string
		event *IssueEvent
	}{
		{
			name:  "not labeled action",
			event: &IssueEvent{Action: "opened", Label: &Label{Name: "backport/v1"}, Issue: Issue{PullRequest: &IssuePullRequestPointer{URL: "u"}}},
		},
		{
			name:  "nil label",
			event: &IssueEvent{Action: "labeled", Label: nil, Issue: Issue{PullRequest: &IssuePullRequestPointer{URL: "u"}}},
		},
		{
			name:  "not a PR issue",
			event: &IssueEvent{Action: "labeled", Label: &Label{Name: "backport/v1"}, Issue: Issue{PullRequest: nil}},
		},
		{
			name:  "non-backport label",
			event: &IssueEvent{Action: "labeled", Label: &Label{Name: "bug"}, Issue: Issue{PullRequest: &IssuePullRequestPointer{URL: "u"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer("secret")
			s.dispatchIssueEvent(tt.event)
			s.WaitAll()
		})
	}
}
