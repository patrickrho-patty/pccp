package pia

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

// TestKVJournalPublishToScheduler verifies the §13.11 loop: the worker
// appends KV events to its journal, the journal publishes over DARI, and
// the scheduler's index dedups and indexes the blocks.
func TestKVJournalPublishToScheduler(t *testing.T) {
	journal, err := OpenKVJournal(filepath.Join(t.TempDir(), "kv.journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if _, err := journal.Append("add", "block-h1", 100); err != nil {
		t.Fatal(err)
	}

	ca, err := dari.NewPeerCredentialIssuer("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	subjectPub, subjectPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := ca.Issue(dari.IssueRequest{
		SubjectPeerID:           "kv-wkr-001",
		Organization:            "test-org",
		Profile:                 dari.ProfileInference,
		PublicKey:               subjectPub,
		Validity:                time.Hour,
		RevocationAuthority:     "test-ca",
		AllowedProtocolVersions: []uint8{1},
		BuildChannel:            "stable",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, configPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	configPub := configPriv.Public().(ed25519.PublicKey)
	engine := fakeEngine(t,
		`{"object":"list","data":[{"id":"Qwen3.6-27B-FP8"}]}`,
		"vllm:num_requests_running 0\nvllm:num_requests_waiting 8\n",
	)
	t.Cleanup(engine.Close)
	envelope, err := scheduler.SignConfig(configPriv, scheduler.PIAWorkerConfig{
		WorkerID:      "kv-wkr-001",
		TenantID:      "tenant-kv",
		BackendMode:   "localhost",
		EngineURL:     engine.URL,
		AllowedModels: []string{"Qwen3.6-27B-FP8"},
	})
	if err != nil {
		t.Fatal(err)
	}

	svc, addr := startScheduler(t, ca, configPub, nil)
	cfg := WorkerAgentConfig{
		SchedulerAddr:  addr,
		Credential:     cred,
		SubjectPriv:    subjectPriv,
		EngineURL:      engine.URL,
		EngineKind:     "vllm",
		SignedConfig:   envelope,
		KVJournal:      journal,
		Heartbeat:      100 * time.Millisecond,
		ReconnectDelay: 50 * time.Millisecond,
	}
	agent, err := NewWorkerAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agent.Run(ctx)

	waitFor(t, 5*time.Second, func() bool {
		return svc.KV.OverlapTokens("kv-wkr-001", "tenant-kv", "block-h1") == 100
	})

	// Replay idempotency: append the same event again with a new seq —
	// the index keeps the latest.
	if _, err := journal.Append("add", "block-h1", 150); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool {
		return svc.KV.OverlapTokens("kv-wkr-001", "tenant-kv", "block-h1") == 150
	})
	_ = json.Marshal
}
