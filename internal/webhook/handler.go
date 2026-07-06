package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sync"

	"github.com/authentik-labs/mewp/internal/cherrypick"
	"github.com/authentik-labs/mewp/internal/config"
)

var backportLabelRe = regexp.MustCompile(`^backport/(.+)$`)

type Server struct {
	cfg    *config.Config
	logger *slog.Logger
	wg     sync.WaitGroup
}

func NewServer(cfg *config.Config, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, logger: logger}
}

// WaitAll blocks until all in-flight cherry-pick goroutines have finished.
// Call this during graceful shutdown after the HTTP server stops accepting requests.
func (s *Server) WaitAll() {
	s.wg.Wait()
}

// ServeHTTP implements http.Handler for POST /webhook.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := verifySignature(s.cfg.WebhookSecret, r)
	if err != nil {
		s.logger.Warn("signature verification failed", "err", err, "remote_addr", r.RemoteAddr)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	s.logger.Info("webhook received", "event", eventType, "delivery", r.Header.Get("X-GitHub-Delivery"), "remote_addr", r.RemoteAddr)
	switch eventType {
	case "pull_request":
		var event PullRequestEvent
		if err := json.Unmarshal(body, &event); err != nil {
			s.logger.Warn("failed to parse pull_request payload", "err", err)
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		s.dispatchPullRequestEvent(&event)
	case "issues":
		var event IssueEvent
		if err := json.Unmarshal(body, &event); err != nil {
			s.logger.Warn("failed to parse issues payload", "err", err)
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		s.dispatchIssueEvent(&event)
	default:
		s.logger.Debug("ignoring event", "event", eventType)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) dispatchPullRequestEvent(event *PullRequestEvent) {
	pr := event.PullRequest
	owner := event.Repository.Owner.Login
	repo := event.Repository.Name

	s.logger.Info("pull_request", "action", event.Action, "repo", owner+"/"+repo, "pr", pr.Number, "title", pr.Title)
	switch event.Action {
	case "closed":
		if !pr.Merged {
			return
		}
		for _, label := range pr.Labels {
			m := backportLabelRe.FindStringSubmatch(label.Name)
			if m == nil {
				continue
			}
			job := cherrypick.NewJob(s.cfg, s.logger, event.Installation.ID, owner, repo,
				pr.Number, pr.Title, pr.User.Login, pr.MergeCommitSHA, m[1])
			s.wg.Add(1)
			go func(j *cherrypick.Job) {
				defer s.wg.Done()
				if err := j.Process(context.Background()); err != nil {
					s.logger.Error("cherry-pick failed", "pr", j.PRNumber, "target", j.TargetBranch, "err", err)
				}
			}(job)
		}

	case "labeled":
		if !pr.Merged || event.Label == nil {
			return
		}
		m := backportLabelRe.FindStringSubmatch(event.Label.Name)
		if m == nil {
			return
		}
		job := cherrypick.NewJob(s.cfg, s.logger, event.Installation.ID, owner, repo,
			pr.Number, pr.Title, pr.User.Login, pr.MergeCommitSHA, m[1])
		s.wg.Add(1)
		go func(j *cherrypick.Job) {
			defer s.wg.Done()
			if err := j.Process(context.Background()); err != nil {
				s.logger.Error("cherry-pick failed", "pr", j.PRNumber, "target", j.TargetBranch, "err", err)
			}
		}(job)
	}
}

// dispatchIssueEvent handles issues.labeled events. GitHub fires these instead of
// pull_request.labeled when a label is added to a PR from an external fork.
func (s *Server) dispatchIssueEvent(event *IssueEvent) {
	owner := event.Repository.Owner.Login
	repo := event.Repository.Name
	s.logger.Info("issues", "action", event.Action, "repo", owner+"/"+repo, "issue", event.Issue.Number)
	if event.Action != "labeled" || event.Issue.PullRequest == nil || event.Label == nil {
		return
	}
	m := backportLabelRe.FindStringSubmatch(event.Label.Name)
	if m == nil {
		return
	}

	installationID := event.Installation.ID
	issueNumber := event.Issue.Number
	targetBranch := m[1]

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		err := cherrypick.ProcessIssueLabel(context.Background(), s.cfg, s.logger,
			installationID, owner, repo, issueNumber, targetBranch)
		if err != nil {
			s.logger.Error("issue backport failed", "issue", issueNumber, "target", targetBranch, "err", err)
		}
	}()
}

func verifySignature(secret string, r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		return nil, fmt.Errorf("missing X-Hub-Signature-256 header")
	}
	if len(sig) < 8 || sig[:7] != "sha256=" {
		return nil, fmt.Errorf("malformed X-Hub-Signature-256 header")
	}
	sigBytes, err := hex.DecodeString(sig[7:])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(sigBytes, mac.Sum(nil)) {
		return nil, fmt.Errorf("signature mismatch")
	}
	return body, nil
}
