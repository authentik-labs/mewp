package cherrypick

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/authentik-labs/mewp/internal/config"
	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v88/github"
)

// ResolveAppGitIdentity fetches the GitHub App's slug and bot user ID, then sets
// cfg.GitUserName and cfg.GitUserEmail to the values GitHub uses for app-authored commits:
//
//	name:  <slug>[bot]
//	email: <id>+<slug>[bot]@users.noreply.github.com
func ResolveAppGitIdentity(ctx context.Context, cfg *config.Config) error {
	appTransport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, cfg.AppID, cfg.AppPrivateKey)
	if err != nil {
		return fmt.Errorf("create app transport: %w", err)
	}
	appClient, err := github.NewClient(github.WithTransport(appTransport))
	if err != nil {
		return fmt.Errorf("create app client: %w", err)
	}

	// GET /app — returns the authenticated app's metadata including its slug.
	app, _, err := appClient.Apps.Get(ctx, "")
	if err != nil {
		return fmt.Errorf("get app info: %w", err)
	}
	botUsername := app.GetSlug() + "[bot]"

	// GET /users/<slug>[bot] — the bot user has a stable numeric ID used in the email.
	// Use an unauthenticated client: /users is public and GitHub rejects App JWTs there.
	anonClient, err := github.NewClient(github.WithHTTPClient(http.DefaultClient))
	if err != nil {
		return fmt.Errorf("create anon client: %w", err)
	}
	user, _, err := anonClient.Users.Get(ctx, botUsername)
	if err != nil {
		return fmt.Errorf("get bot user %q: %w", botUsername, err)
	}

	cfg.GitUserName = botUsername
	cfg.GitUserEmail = fmt.Sprintf("%d+%s@users.noreply.github.com", user.GetID(), botUsername)
	return nil
}

func newGitHubClient(cfg *config.Config, installationID int64) (*github.Client, error) {
	itr, err := ghinstallation.New(http.DefaultTransport, cfg.AppID, installationID, cfg.AppPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("create installation transport: %w", err)
	}
	client, err := github.NewClient(github.WithTransport(itr))
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}
	return client, nil
}

func getInstallationToken(ctx context.Context, cfg *config.Config, installationID int64) (string, error) {
	itr, err := ghinstallation.New(http.DefaultTransport, cfg.AppID, installationID, cfg.AppPrivateKey)
	if err != nil {
		return "", fmt.Errorf("create installation transport: %w", err)
	}
	return itr.Token(ctx)
}

func branchExists(ctx context.Context, client *github.Client, owner, repo, branch string) (bool, error) {
	_, resp, err := client.Git.GetRef(ctx, owner, repo, "heads/"+branch)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("get ref: %w", err)
	}
	return true, nil
}

// GetPR fetches a pull request by number. Exported for use by the webhook package
// when handling issues.labeled events (which don't include PR details in the payload).
func GetPR(ctx context.Context, cfg *config.Config, installationID int64, owner, repo string, number int) (*github.PullRequest, error) {
	client, err := newGitHubClient(cfg, installationID)
	if err != nil {
		return nil, err
	}
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("get PR #%d: %w", number, err)
	}
	return pr, nil
}

func prExistsForBranch(ctx context.Context, client *github.Client, owner, repo, headBranch string) (int, error) {
	prs, _, err := client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		State: "open",
		Head:  owner + ":" + headBranch,
	})
	if err != nil {
		return 0, fmt.Errorf("list PRs: %w", err)
	}
	if len(prs) > 0 {
		return prs[0].GetNumber(), nil
	}
	return 0, nil
}

func createCherryPickPR(ctx context.Context, logger *slog.Logger, client *github.Client, owner, repo, title, body, base, head, author string) (string, error) {
	pr, _, err := client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
		Title: github.Ptr(title),
		Body:  github.Ptr(body),
		Base:  github.Ptr(base),
		Head:  github.Ptr(head),
	})
	if err != nil {
		return "", fmt.Errorf("create PR: %w", err)
	}

	prNumber := pr.GetNumber()

	if _, _, err := client.Issues.AddLabelsToIssue(ctx, owner, repo, prNumber, []string{"cherry-pick"}); err != nil {
		logger.Warn("add label to cherry-pick PR", "pr", prNumber, "err", err)
	}

	if _, _, err := client.Issues.AddAssignees(ctx, owner, repo, prNumber, []string{author}); err != nil {
		logger.Warn("add assignee to cherry-pick PR", "pr", prNumber, "err", err)
	}

	return pr.GetHTMLURL(), nil
}

func commentOnPR(ctx context.Context, client *github.Client, owner, repo string, prNumber int, body string) error {
	_, _, err := client.Issues.CreateComment(ctx, owner, repo, prNumber, &github.IssueComment{
		Body: github.Ptr(body),
	})
	if err != nil {
		return fmt.Errorf("create comment on PR #%d: %w", prNumber, err)
	}
	return nil
}
