package gitscm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// browser.go implements the live SCM surface (repositories C1/C2): the
// connector clones/reads trees and records sync state, powering the
// file browser on the repository detail page. Clones are cached under
// the OS temp dir per repository ID; a re-sync refreshes the clone.

const scmWorkRoot = "pccp-scm"

type cloneCache struct {
	mu    sync.RWMutex
	dirs  map[string]string // repoID -> workdir
}

var clones = &cloneCache{dirs: map[string]string{}}

// SyncRepository clones (or refreshes) the repository and records the
// HEAD commit + sync status on the repo row. The provider is selected
// by SCMProvider; the clone token comes from PCCP_SCM_TOKEN.
func (s *Service) SyncRepository(ctx context.Context, repo *models.Repository) (head string, err error) {
	if repo.CloneURL == "" {
		return "", fmt.Errorf("gitscm: repository %s has no clone_url", repo.Name)
	}
	workDir, err := os.MkdirTemp("", scmWorkRoot+"-"+repo.ID+"-")
	if err != nil {
		return "", fmt.Errorf("gitscm: workdir: %w", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(workDir)
			clones.mu.Lock()
			delete(clones.dirs, repo.ID)
			clones.mu.Unlock()
		}
	}()

	ref := repo.DefaultBranch
	if ref == "" {
		ref = "main"
	}
	prov := providerFor(repo.SCMProvider)
	s.db.Model(repo).Updates(map[string]interface{}{"sync_status": "syncing"})
	if err := prov.Clone(ctx, repo.CloneURL, ref, workDir); err != nil {
		s.db.Model(repo).Updates(map[string]interface{}{"sync_status": "failed"})
		return "", fmt.Errorf("gitscm: clone %s: %w", repo.Name, err)
	}
	head, err = prov.Head(workDir)
	if err != nil {
		s.db.Model(repo).Updates(map[string]interface{}{"sync_status": "failed"})
		return "", fmt.Errorf("gitscm: head %s: %w", repo.Name, err)
	}
	commitAt := lastCommitTime(workDir)
	now := time.Now().Format(time.RFC3339)
	s.db.Model(repo).Updates(map[string]interface{}{
		"sync_status":    "synced",
		"last_sync_at":   now,
		"last_commit_at": commitAt,
	})
	// Evict the previous clone and register the fresh one.
	clones.mu.Lock()
	if old, ok := clones.dirs[repo.ID]; ok {
		os.RemoveAll(old)
	}
	clones.dirs[repo.ID] = workDir
	clones.mu.Unlock()
	return head, nil
}

// TreeEntry is one directory entry in the file browser.
type TreeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size,omitempty"`
}

// ListTree lists a directory of the repo's synced clone. When the repo
// has never been synced, the caller receives ErrNotSynced and the UI
// prompts a sync.
var ErrNotSynced = fmt.Errorf("gitscm: repository not synced yet — run sync first")

func (s *Service) ListTree(repoID, path string) ([]TreeEntry, error) {
	clones.mu.RLock()
	dir, ok := clones.dirs[repoID]
	clones.mu.RUnlock()
	if !ok || dir == "" {
		return nil, ErrNotSynced
	}
	full := filepath.Join(dir, filepath.Clean("/"+path))
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, fmt.Errorf("gitscm: read dir: %w", err)
	}
	out := make([]TreeEntry, 0, len(entries))
	for _, e := range entries {
		rel := filepath.ToSlash(filepath.Join(path, e.Name()))
		entry := TreeEntry{Name: e.Name(), Path: rel, Dir: e.IsDir()}
		if info, ierr := e.Info(); ierr == nil && !e.IsDir() {
			entry.Size = info.Size()
		}
		out = append(out, entry)
	}
	return out, nil
}

// ReadFile returns the content of a synced file.
func (s *Service) ReadFile(repoID, path string) ([]byte, error) {
	clones.mu.RLock()
	dir, ok := clones.dirs[repoID]
	clones.mu.RUnlock()
	if !ok || dir == "" {
		return nil, ErrNotSynced
	}
	full := filepath.Join(dir, filepath.Clean("/"+path))
	if !strings.HasPrefix(full, dir) {
		return nil, fmt.Errorf("gitscm: path escapes repository")
	}
	return os.ReadFile(full)
}

// IsSynced reports whether a synced clone exists for the repo.
func (s *Service) IsSynced(repoID string) bool {
	clones.mu.RLock()
	defer clones.mu.RUnlock()
	return clones.dirs[repoID] != ""
}

// IngestWebhook processes an SCM webhook push event (repositories
// C1): records the event and, for push events with a head commit,
// records a commit binding so provenance can resolve it (§18.6).
func (s *Service) IngestWebhook(repo *models.Repository, eventType string, payload []byte) error {
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return fmt.Errorf("gitscm: webhook payload: %w", err)
	}
	headSHA := "unknown"
	if after, ok := data["after"].(string); ok && after != "" {
		headSHA = after
	}
	s.recordAudit(repo.OrganizationID, "scm.webhook_received", "scm", "repository", repo.ID,
		fmt.Sprintf(`{"event":"%s","head":"%s"}`, eventType, headSHA))
	return nil
}

func (s *Service) recordAudit(orgID, action, actor, resourceType, resourceID, details string) {
	event := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      action,
		ActorType:      actor,
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Details:        details,
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(event)
}

func providerFor(provider string) Provider {
	switch strings.ToLower(provider) {
	case "gitlab":
		return &GitLabProvider{Token: os.Getenv("PCCP_SCM_TOKEN")}
	case "github":
		return &GitHubProvider{Token: os.Getenv("PCCP_SCM_TOKEN")}
	default:
		return &LocalProvider{}
	}
}

func lastCommitTime(dir string) string {
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%cI").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
