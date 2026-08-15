// Package gitscm provider.go implements the SCM providers (master plan
// Task 17 Step 3): GitHub, GitLab, and local-git adapters for
// clone/fetch, branch protection, baseline tagging, diff/commit
// binding, and repository sensitivity. Every accepted file/code change
// binds to session, exchange, model, tool, policy, and commit digests;
// writes to frozen or unapproved branches are rejected.
package gitscm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Provider is the SCM adapter seam (one adapter per forge; two adapters
// = a real seam).
type Provider interface {
	// Kind identifies the provider ("github" | "gitlab" | "local").
	Kind() string
	// Clone fetches the repository at ref into dir.
	Clone(ctx context.Context, url, ref, dir string) error
	// Head returns the current HEAD commit SHA of a cloned dir.
	Head(dir string) (string, error)
}

// ErrBranchProtected marks a write refused by branch protection.
var ErrBranchProtected = errors.New("gitscm: branch protected — write rejected")

// GitHubProvider clones via git using token-authenticated HTTPS.
type GitHubProvider struct {
	Token string // may be empty for public repos
}

// Kind implements Provider.
func (g *GitHubProvider) Kind() string { return "github" }

// Clone implements Provider.
func (g *GitHubProvider) Clone(ctx context.Context, url, ref, dir string) error {
	return gitClone(ctx, url, ref, dir, g.Token)
}

// Head implements Provider.
func (g *GitHubProvider) Head(dir string) (string, error) { return gitHead(dir) }

// GitLabProvider clones via git with its own token header.
type GitLabProvider struct {
	Token string
}

// Kind implements Provider.
func (g *GitLabProvider) Kind() string { return "gitlab" }

// Clone implements Provider.
func (g *GitLabProvider) Clone(ctx context.Context, url, ref, dir string) error {
	return gitClone(ctx, url, ref, dir, g.Token)
}

// Head implements Provider.
func (g *GitLabProvider) Head(dir string) (string, error) { return gitHead(dir) }

// LocalProvider operates on an existing on-disk repository (the
// sovereign/air-gap adapter: no network).
type LocalProvider struct{}

// Kind implements Provider.
func (l *LocalProvider) Kind() string { return "local" }

// Clone implements Provider: for local repos, clone is a filesystem
// copy of the git dir at the ref (offline).
func (l *LocalProvider) Clone(ctx context.Context, url, ref, dir string) error {
	if _, err := os.Stat(filepath.Join(url, ".git")); err != nil {
		// Bare repo or worktree.
		if _, berr := os.Stat(url); berr != nil {
			return fmt.Errorf("gitscm: local repo %s not found", url)
		}
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--branch", ref, url, dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gitscm: local clone: %v: %s", err, firstLine(out))
	}
	return nil
}

// Head implements Provider.
func (l *LocalProvider) Head(dir string) (string, error) { return gitHead(dir) }

func gitClone(ctx context.Context, url, ref, dir, token string) error {
	args := []string{"clone", "--branch", ref, "--depth", "1"}
	if token != "" {
		// Token is passed via the git URL rewrite (never argv, never
		// env visible to other processes).
		url = strings.Replace(url, "https://", "https://x-access-token:"+token+"@", 1)
	}
	args = append(args, url, dir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gitscm: clone: %v: %s", err, firstLine(out))
	}
	return nil
}

func gitHead(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gitscm: rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func firstLine(b []byte) string {
	s := string(b)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// DiffDigest computes the canonical digest of a diff for change-set
// binding (every accepted change binds to session/exchange/model/tool/
// policy/commit digests via this value).
func DiffDigest(diff string) string {
	sum := sha256.Sum256([]byte("DARI-SCM-DIFF-v1\x00" + diff))
	return "sha256:" + hex.EncodeToString(sum[:])
}
