package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"os"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"github.com/patrickrho-patty/pccp/internal/webbinding"
)

// webbinding_bridge.go is the relay's dari.web/1 carrier bridge: the
// executor serving EFFECT_STATUS and the governed envelope handler.

// webEffectExecutor is the relay-owned effect executor serving
// dari.web/1 EFFECT_STATUS queries with SIGNED status responses
// (F.10: a reconnecting caller queries status; it never re-executes).
type webEffectExecutor struct {
	executor *dari.EffectExecutor
	priv     ed25519.PrivateKey
}

// NewWebBindingServer builds the dari.web/1 carrier over this relay's
// governed path: every inbound envelope routes through GovernInference
// with the session's grant — the browser path never bypasses governance.
func (s *Service) NewWebBindingServer(allowedOrigins []string) (*webbinding.Server, error) {
	store, err := webbinding.NewSessionStore("")
	if err != nil {
		return nil, err
	}
	fx := &webEffectExecutor{
		executor: dari.NewDurableEffectExecutor(s.relayID, s.policy.SigningPrivateKey(), &effectStoreDB{db: s.db}),
		priv:     s.policy.SigningPrivateKey(),
	}
	if err := s.db.AutoMigrate(&effectRecordRow{}); err != nil {
		return nil, fmt.Errorf("webbinding: effect store migrate: %w", err)
	}
	return webbinding.NewServer(store, allowedOrigins, func(sessionID string, envelope []byte) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Bind the browser session to the governed chain (harness +
		// lease) before any inference — subject identity comes from
		// the session's verified proof thumbprint.
		sess, ok := store.Get(sessionID)
		if !ok {
			return nil, fmt.Errorf("webbinding: unknown session")
		}
		// Effect-status queries route to the durable executor view with
		// a SIGNED response (never fabricated).
		if opID, ok := strings.CutPrefix(string(envelope), "EFFECT_STATUS:"); ok {
			return fx.status(opID)
		}
		// Governed AI exchange under the session's bound harness.
		model0, _, _, perr := parseAIOpen(envelope)
		if perr != nil {
			return nil, fmt.Errorf("webbinding: not an AI_OPEN envelope: %w", perr)
		}
		harnessID, _, gerr := s.EnsureWebSessionGovernance(sess.SubjectThumb, sessionID, model0)
		if gerr != nil {
			return nil, gerr
		}
		return s.governWebEnvelope(ctx, harnessID, sessionID, envelope)
	})
}

// status returns the signed F.10 status response for an operation
// (ABSENT for unknown operations — honest, never fabricated). The
// response is shape-validated and kernel-signed.
func (w *webEffectExecutor) status(opID string) ([]byte, error) {
	state, result, ok := w.executor.Status(opID)
	if !ok {
		state = dari.EffectStateAbsent
	}
	body := &dari.EffectStatusBody{
		Version:     1,
		OperationID: opID,
		Kind:        2,
		State:       &state,
	}
	if result != nil && result.Body != nil {
		pd := result.Body.PrepareDigest
		body.PrepareDigest = &pd
	}
	env, err := dari.SignEffectStatus(body, w.priv)
	if err != nil {
		return nil, err
	}
	return env.COSEBytes, nil
}

// parseAIOpen is the single AI_OPEN payload parser shared by the
// native listener and the web carrier (defaulting cannot diverge).
func parseAIOpen(payload []byte) (model string, msgs []map[string]string, maxTokens int, err error) {
	var aiReq struct {
		Model     string                   `json:"model"`
		Messages  []map[string]interface{} `json:"messages"`
		MaxTokens int                      `json:"max_tokens"`
	}
	if err := json.Unmarshal(payload, &aiReq); err != nil {
		return "", nil, 0, fmt.Errorf("decode: %w", err)
	}
	msgs = make([]map[string]string, 0, len(aiReq.Messages))
	for _, m := range aiReq.Messages {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		msgs = append(msgs, map[string]string{"role": role, "content": content})
	}
	if aiReq.MaxTokens <= 0 {
		aiReq.MaxTokens = 4096
	}
	return aiReq.Model, msgs, aiReq.MaxTokens, nil
}

// EnsureWebSessionGovernance binds a verified browser session to the
// SAME governed chain the native transport uses (map §10: identical
// authorization semantics — no ungoverned web path exists):
// find-or-create the web org + a harness identity keyed by the
// subject-key thumbprint, register model serving, bind the active
// policy epoch, and issue the session's capability lease. Idempotent
// per (subject, session, model) — a reconnect reuses the chain.
func (s *Service) EnsureWebSessionGovernance(subjectThumb [32]byte, sessionID, model string) (harnessID, orgID string, err error) {
	orgID = s.webOrg()
	harnessID = "web-" + hex.EncodeToString(subjectThumb[:8])

	// Harness identity for the browser subject (idempotent).
	var h models.Harness
	if err := s.db.Where("harness_id = ?", harnessID).First(&h).Error; err != nil {
		h = models.Harness{
			OrganizationID: orgID, HarnessID: harnessID,
			DeviceID: "browser", BinaryVersion: "web", Status: "enrolled",
		}
		if err := s.db.Create(&h).Error; err != nil {
			return "", "", fmt.Errorf("webbinding: enroll browser harness: %w", err)
		}
	}

	// Serving chain + epoch (fail-closed outside dev bootstrap, same
	// as the native SESSION_OPEN path).
	pkg, err := s.RegisterModelServing(orgID, model)
	if err != nil {
		return "", "", fmt.Errorf("webbinding: model serving: %w", err)
	}
	epoch, eerr := s.Policy().GetActiveEpoch(orgID)
	if eerr != nil {
		if !devBootstrap() {
			return "", "", fmt.Errorf("webbinding: no active policy epoch: %w", eerr)
		}
		epoch, eerr = s.Policy().CreatePolicyEpoch(orgID, []string{pkg.PackageID}, "immediate")
		if eerr != nil {
			return "", "", fmt.Errorf("webbinding: bootstrap epoch: %w", eerr)
		}
		s.hotState.InvalidateAll()
	} else if !epochAllowsPackage(epoch, pkg.PackageID) {
		return "", "", fmt.Errorf("webbinding: model not allowed under the active policy epoch")
	}

	// Session lease (idempotent by session).
	var existing models.CapabilityLease
	q := s.db.Where("subject_peer_id = ? AND session_id = ? AND status = 'active'", harnessID, "web-"+sessionID)
	if err := q.First(&existing).Error; err == nil {
		return harnessID, orgID, nil
	}
	lease, lerr := s.Policy().IssueCapabilityLease(policy.IssueLeaseRequest{
		OrganizationID: orgID,
		SubjectPeerID:  harnessID,
		UserID:         "browser",
		SessionID:      "web-" + sessionID,
		PolicyEpochID:  epoch.EpochID,
		AllowedModels:  []string{model},
		Validity:       12 * time.Hour,
		TokenBudget:    1 << 22,
	})
	if lerr != nil {
		return "", "", fmt.Errorf("webbinding: lease: %w", lerr)
	}
	_ = lease
	return harnessID, orgID, nil
}

// webOrg resolves the org browser sessions belong to: the configured
// web org, the single existing org, or a fresh org-web on a fresh
// deployment.
func (s *Service) webOrg() string {
	if id := os.Getenv("PCCP_WEB_ORG_ID"); id != "" {
		return id
	}
	var orgs []models.Organization
	s.db.Order("created_at ASC").Find(&orgs)
	for _, o := range orgs {
		if o.Status == "active" {
			return o.ID
		}
	}
	web := models.Organization{
		Name: "Web Sessions", Slug: "org-web", Type: "enterprise",
		Profile: "public", Status: "active",
	}
	if err := s.db.Create(&web).Error; err == nil {
		return web.ID
	}
	return "org-web"
}

// governWebEnvelope runs the governed exchange for a bound web
// session's AI_OPEN envelope under its harness identity.
func (s *Service) governWebEnvelope(ctx context.Context, harnessID, sessionID string, envelope []byte) ([]byte, error) {
	model, msgs, maxTokens, err := parseAIOpen(envelope)
	if err != nil {
		return nil, fmt.Errorf("webbinding: not an AI_OPEN envelope: %w", err)
	}
	resp, receipt, err := s.GovernInference(ctx, GovernRequest{
		HarnessID: harnessID,
		SessionID: "web-" + sessionID,
		Model:     model,
		Messages:  msgs,
		MaxTokens: maxTokens,
	})
	if err != nil {
		return nil, err
	}
	// Governance footer data (honest UI): the exchange's evidence
	// receipt ID + chain root accompany every response.
	raw, merr := json.Marshal(resp)
	if merr != nil {
		return nil, merr
	}
	var withReceipt map[string]any
	if jerr := json.Unmarshal(raw, &withReceipt); jerr == nil {
		if receipt != nil {
			withReceipt["receipt_id"] = "rcpt-" + receipt.ExchangeID
			withReceipt["chain_root"] = receipt.ChainRoot
		}
		out, _ := json.Marshal(withReceipt)
		return out, nil
	}
	return raw, nil
}

// effectStoreDB persists effect records for the web executor (F.10
// durability): EFFECT_STATUS answers from history after a relay
// restart instead of ABSENT.
type effectStoreDB struct{ db *gorm.DB }

// effectRecordRow is the storage model (dari.EffectRecordRow columns).
type effectRecordRow struct {
	ID            string `gorm:"type:varchar(128);primaryKey"`
	State         int
	Nonce         []byte
	PrepareDigest string
	GrantDigest   string
	InputDigest   string
	AuthDigest    string
	Executor      string
	RetryOwner    string
	ResultCOSE    []byte
}

func (effectRecordRow) TableName() string { return "effect_records" }

func (s *effectStoreDB) SaveEffect(opID string, rec *dari.EffectRecordRow) error {
	if s.db == nil || rec == nil {
		return nil
	}
	row := effectRecordRow{
		ID: opID, State: int(rec.State), Nonce: rec.Nonce[:],
		PrepareDigest: hex.EncodeToString(rec.PrepareDigest[:]),
		GrantDigest:   hex.EncodeToString(rec.GrantDigest[:]),
		InputDigest:   hex.EncodeToString(rec.InputDigest[:]),
		AuthDigest:    hex.EncodeToString(rec.AuthDigest[:]),
		Executor:      rec.Executor, RetryOwner: rec.RetryOwner,
		ResultCOSE: rec.ResultCOSE,
	}
	return s.db.Save(&row).Error
}

func (s *effectStoreDB) LoadEffect(opID string) (*dari.EffectRecordRow, error) {
	if s.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var row effectRecordRow
	if err := s.db.Where("id = ?", opID).First(&row).Error; err != nil {
		return nil, err
	}
	out := &dari.EffectRecordRow{
		State: dari.EffectState(row.State), Executor: row.Executor,
		RetryOwner: row.RetryOwner, ResultCOSE: row.ResultCOSE,
	}
	copy(out.Nonce[:], row.Nonce)
	copy(out.PrepareDigest[:], mustHex(row.PrepareDigest))
	copy(out.GrantDigest[:], mustHex(row.GrantDigest))
	copy(out.InputDigest[:], mustHex(row.InputDigest))
	copy(out.AuthDigest[:], mustHex(row.AuthDigest))
	return out, nil
}

func mustHex(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}
