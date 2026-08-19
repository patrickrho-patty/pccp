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
	mu   sync.RWMutex
	dirs map[string]string // repoID -> workdir
}

var clones = &cloneCache{dirs: map[string]string{}}

// syncStaleAfter bounds how long a 'syncing' row is trusted. A crash
// mid-clone would otherwise leave sync_status='syncing' forever and
// deadlock every future sync; a syncing row with no recorded attempt or
// an attempt older than this is treated as failed and may be reclaimed.
const syncStaleAfter = 15 * time.Minute

// SyncRepository clones (or refreshes) the repository and records the
// HEAD commit + sync status on the repo row. The provider is selected
// by SCMProvider; the clone token comes from PCCP_SCM_TOKEN. Both the
// latest attempt (attempt_at/head/error) and the last success are
// persisted so the UI can reconcile list and detail state (PAT-1493).
func (s *Service) SyncRepository(ctx context.Context, repo *models.Repository) (head string, err error) {
	if repo.CloneURL == "" {
		return "", fmt.Errorf("gitscm: repository %s has no clone_url", repo.Name)
	}
	// Idempotence: claim the repo with one atomic conditional UPDATE so
	// two concurrent syncs cannot both pass a check-then-set race.
	attemptAt := time.Now().Format(time.RFC3339)
	staleBefore := time.Now().Add(-syncStaleAfter).Format(time.RFC3339)
	claim := s.db.Model(&models.Repository{}).
		Where("id = ?", repo.ID).
		Where("(sync_status <> ? OR sync_status IS NULL OR last_sync_attempt_at = '' OR last_sync_attempt_at < ?)", "syncing", staleBefore).
		Updates(map[string]interface{}{"sync_status": "syncing", "last_sync_attempt_at": attemptAt})
	if claim.Error != nil {
		return "", fmt.Errorf("gitscm: claim sync for %s: %w", repo.Name, claim.Error)
	}
	if claim.RowsAffected == 0 {
		return "", fmt.Errorf("gitscm: sync already in progress for %s", repo.Name)
	}
	fail := func(err error) (string, error) {
		s.db.Model(repo).Updates(map[string]interface{}{
			"sync_status": "failed", "last_sync_attempt_at": attemptAt, "last_sync_error": err.Error(),
		})
		return "", err
	}
	workDir, err := os.MkdirTemp("", scmWorkRoot+"-"+repo.ID+"-")
	if err != nil {
		return fail(fmt.Errorf("gitscm: workdir: %w", err))
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
	if err := prov.Clone(ctx, repo.CloneURL, ref, workDir); err != nil {
		return fail(fmt.Errorf("gitscm: clone %s: %w", repo.Name, err))
	}
	head, err = prov.Head(workDir)
	if err != nil {
		return fail(fmt.Errorf("gitscm: head %s: %w", repo.Name, err))
	}
	commitAt := lastCommitTime(workDir)
	now := time.Now().Format(time.RFC3339)
	s.db.Model(repo).Updates(map[string]interface{}{
		"sync_status":     "synced",
		"last_sync_at":    now,
		"last_commit_at":  commitAt,
		"last_sync_head":  head,
		"last_sync_error": "",
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
	dir := cloneDir(repoID)
	if dir == "" {
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
	dir := cloneDir(repoID)
	if dir == "" {
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
	return cloneDir(repoID) != ""
}

// cloneDir resolves the repo's synced clone workdir. On a cache miss it
// restores the newest surviving clone from the OS temp dir so a server
// restart does not strand rows marked synced (PAT-1493).
func cloneDir(repoID string) string {
	clones.mu.RLock()
	dir := clones.dirs[repoID]
	clones.mu.RUnlock()
	if dir != "" {
		return dir
	}
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), scmWorkRoot+"-"+repoID+"-*"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	// Prefer the most recently modified clone directory.
	best := ""
	var bestMod time.Time
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || !info.IsDir() {
			continue
		}
		if best == "" || info.ModTime().After(bestMod) {
			best, bestMod = m, info.ModTime()
		}
	}
	if best == "" {
		return ""
	}
	clones.mu.Lock()
	clones.dirs[repoID] = best
	clones.mu.Unlock()
	return best
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
