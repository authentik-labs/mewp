package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Clone performs a blobless partial clone. The full commit graph is fetched
// but file blobs are lazy-loaded on checkout, avoiding shallow-history issues
// when cherry-picking older commits.
func Clone(ctx context.Context, cloneURL, dir string) error {
	_, err := runGit(ctx, "", "clone", "--filter=blob:none", "--no-checkout", cloneURL, dir)
	return err
}

func ConfigUser(ctx context.Context, dir, name, email string) error {
	if _, err := runGit(ctx, dir, "config", "user.name", name); err != nil {
		return err
	}
	_, err := runGit(ctx, dir, "config", "user.email", email)
	return err
}

func FetchAndCheckout(ctx context.Context, dir, targetBranch, newBranch string) error {
	if _, err := runGit(ctx, dir, "fetch", "origin", targetBranch); err != nil {
		return fmt.Errorf("fetch target branch: %w", err)
	}
	if _, err := runGit(ctx, dir, "checkout", "-b", newBranch, "origin/"+targetBranch); err != nil {
		return fmt.Errorf("checkout cherry-pick branch: %w", err)
	}
	return nil
}

// CherryPick attempts to cherry-pick sha onto the current branch.
// Returns (false, nil) on success, (true, nil) if there are conflicts,
// or (false, err) for unexpected failures.
func CherryPick(ctx context.Context, dir, sha string) (hasConflicts bool, err error) {
	// Try -m 1 first: required for merge commits (standard GitHub merge strategy).
	out, err := runGit(ctx, dir, "cherry-pick", "-m", "1", sha)
	if err == nil {
		return false, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
		// Not a merge commit (squash/rebase merge); retry without -m 1.
		out, err = runGit(ctx, dir, "cherry-pick", sha)
	}
	if err == nil {
		return false, nil
	}
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, fmt.Errorf("cherry-pick: %w\n%s", err, out)
}

func CommitConflicts(ctx context.Context, dir, message string) error {
	if _, err := runGit(ctx, dir, "add", "."); err != nil {
		return fmt.Errorf("git add after conflict: %w", err)
	}
	if _, err := runGit(ctx, dir, "commit", "-m", message); err != nil {
		return fmt.Errorf("commit conflicts: %w", err)
	}
	return nil
}

func Push(ctx context.Context, dir, branch string) error {
	_, err := runGit(ctx, dir, "push", "origin", branch)
	return err
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	output := redactToken(buf.String())
	if err != nil {
		return output, fmt.Errorf("git %s: %w\n%s", args[0], err, output)
	}
	return output, nil
}

func redactToken(s string) string {
	for {
		start := strings.Index(s, "https://x-access-token:")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "@")
		if end == -1 {
			break
		}
		s = s[:start] + "https://x-access-token:***" + s[start+end:]
	}
	return s
}
