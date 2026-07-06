package cherrypick

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/authentik-labs/mewp/internal/config"
	"github.com/authentik-labs/mewp/internal/gitops"
)

type Job struct {
	InstallationID int64
	Owner          string
	Repo           string
	PRNumber       int
	PRTitle        string
	PRAuthor       string
	MergeCommitSHA string
	TargetBranch   string

	cfg    *config.Config
	logger *slog.Logger
}

func NewJob(cfg *config.Config, logger *slog.Logger, installationID int64, owner, repo string, prNumber int, prTitle, prAuthor, mergeCommitSHA, targetBranch string) *Job {
	return &Job{
		InstallationID: installationID,
		Owner:          owner,
		Repo:           repo,
		PRNumber:       prNumber,
		PRTitle:        prTitle,
		PRAuthor:       prAuthor,
		MergeCommitSHA: mergeCommitSHA,
		TargetBranch:   targetBranch,
		cfg:            cfg,
		logger:         logger.With("pr", prNumber, "target", targetBranch),
	}
}

// ProcessIssueLabel handles an issues.labeled event where the issue is a PR.
// GitHub fires this instead of pull_request.labeled for PRs from external forks.
// It fetches full PR data (not present in the issues payload) before dispatching.
func ProcessIssueLabel(ctx context.Context, cfg *config.Config, logger *slog.Logger, installationID int64, owner, repo string, issueNumber int, targetBranch string) error {
	pr, err := GetPR(ctx, cfg, installationID, owner, repo, issueNumber)
	if err != nil {
		return fmt.Errorf("get PR: %w", err)
	}
	if !pr.GetMerged() {
		logger.Info("PR not yet merged, skipping", "pr", issueNumber)
		return nil
	}
	job := NewJob(cfg, logger, installationID, owner, repo,
		issueNumber, pr.GetTitle(), pr.GetUser().GetLogin(), pr.GetMergeCommitSHA(), targetBranch)
	return job.Process(ctx)
}

func (j *Job) Process(ctx context.Context) error {
	token, err := getInstallationToken(ctx, j.cfg, j.InstallationID)
	if err != nil {
		return fmt.Errorf("get installation token: %w", err)
	}

	client, err := newGitHubClient(j.cfg, j.InstallationID)
	if err != nil {
		return fmt.Errorf("create github client: %w", err)
	}

	exists, err := branchExists(ctx, client, j.Owner, j.Repo, j.TargetBranch)
	if err != nil {
		return fmt.Errorf("check branch: %w", err)
	}
	if !exists {
		_ = commentOnPR(ctx, client, j.Owner, j.Repo, j.PRNumber,
			fmt.Sprintf("⚠️ Cannot backport to `%s`: branch does not exist.", j.TargetBranch))
		return nil
	}

	cherryPickBranch := fmt.Sprintf("cherry-pick/%d-to-%s", j.PRNumber, j.TargetBranch)

	existingPR, err := prExistsForBranch(ctx, client, j.Owner, j.Repo, cherryPickBranch)
	if err != nil {
		return fmt.Errorf("check existing PR: %w", err)
	}
	if existingPR != 0 {
		_ = commentOnPR(ctx, client, j.Owner, j.Repo, j.PRNumber,
			fmt.Sprintf("Cherry-pick to `%s` already exists: #%d", j.TargetBranch, existingPR))
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "cherry-pick-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		err := os.RemoveAll(tmpDir)
		if err != nil {
			j.logger.Warn("failed to remove tmp dir", "err", err)
		}
	}()

	cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, j.Owner, j.Repo)

	if err := gitops.Clone(ctx, cloneURL, tmpDir); err != nil {
		return fmt.Errorf("clone repo: %w", err)
	}
	if err := gitops.ConfigUser(ctx, tmpDir, j.cfg.GitUserName, j.cfg.GitUserEmail); err != nil {
		return err
	}
	if err := gitops.FetchAndCheckout(ctx, tmpDir, j.TargetBranch, cherryPickBranch); err != nil {
		return err
	}

	hasConflicts, err := gitops.CherryPick(ctx, tmpDir, j.MergeCommitSHA)
	if err != nil {
		return fmt.Errorf("cherry-pick: %w", err)
	}

	if hasConflicts {
		j.logger.Info("cherry-pick produced conflicts, creating conflict-resolution PR")
		msg := fmt.Sprintf(
			"Cherry-pick #%d to %s (with conflicts)\n\nThis cherry-pick has conflicts that need manual resolution.\n\nOriginal PR: #%d\nOriginal commit: %s",
			j.PRNumber, j.TargetBranch, j.PRNumber, j.MergeCommitSHA,
		)
		if err := gitops.CommitConflicts(ctx, tmpDir, msg); err != nil {
			return err
		}
	}

	if err := gitops.Push(ctx, tmpDir, cherryPickBranch); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	title := fmt.Sprintf("%s (cherry-pick #%d to %s)", j.PRTitle, j.PRNumber, j.TargetBranch)
	newPRURL, err := createCherryPickPR(ctx, j.logger, client, j.Owner, j.Repo, title, j.buildPRBody(hasConflicts),
		j.TargetBranch, cherryPickBranch, j.PRAuthor)
	if err != nil {
		return fmt.Errorf("create cherry-pick PR: %w", err)
	}

	comment := fmt.Sprintf("🍒 Cherry-pick to `%s` created: %s", j.TargetBranch, newPRURL)
	if hasConflicts {
		comment = fmt.Sprintf("⚠️ Cherry-pick to `%s` has conflicts: %s", j.TargetBranch, newPRURL)
	}
	_ = commentOnPR(ctx, client, j.Owner, j.Repo, j.PRNumber, comment)

	j.logger.Info("cherry-pick PR created", "url", newPRURL)
	return nil
}

func (j *Job) buildPRBody(hasConflicts bool) string {
	base := fmt.Sprintf(
		"Cherry-pick of #%d to `%s` branch.\n\n**Original PR:** #%d\n**Original Author:** @%s\n**Cherry-picked commit:** %s",
		j.PRNumber, j.TargetBranch, j.PRNumber, j.PRAuthor, j.MergeCommitSHA,
	)
	if hasConflicts {
		return "⚠️ **This cherry-pick has conflicts that require manual resolution.**\n\n" +
			base + "\n\n**Please resolve the conflicts in this PR before merging.**"
	}
	return base
}
