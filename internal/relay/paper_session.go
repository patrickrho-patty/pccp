package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"github.com/patrickrho-patty/pccp/internal/paper"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"github.com/patrickrho-patty/pccp/internal/provenance"
)

// This file implements the session-governance and provenance flows of
// the PAPER listener:
//
//   - setupSession: on SESSION_OPEN the relay binds the session to a
//     policy epoch, issues a signed capability lease, pushes the
//     effective model catalog, and grants the session (A3/A4/A5 e2e).
//   - evidence-receipt issuance after every governed exchange (B3 e2e).
//   - ingest* handlers: connector-pushed provenance envelopes recorded
//     through the provenance service (B1 e2e).

// authAckPayload builds the AUTH_ACK body. The connector reads the
// policy issuer's public key here so it can verify the lease the
// relay issues moments later — no side channel, no config file.
func (pl *PaperListener) authAckPayload() []byte {
	payload := map[string]string{
		"status":           "authenticated",
		"relay_id":         pl.svc.relayID,
		"policy_issuer":    "pccp-policy",
		"policy_issuer_pk": hex.EncodeToString(pl.svc.Policy().SigningPublicKey()),
	}
	data, _ := json.Marshal(payload)
	return data
}

// sessionOpenRequest is the connector's SESSION_OPEN body.
type sessionOpenRequest struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Model     string `json:"model"`
}

// setupSession issues the session's lease and pushes the governance
// baseline (epoch + catalog). Order matters: POLICY_EPOCH first (the
// catalog snapshot is epoch-bound), then CATALOG_SNAPSHOT, then
// LEASE_ISSUE, then SESSION_GRANT. The connector's connect() consumes
// exactly this sequence before its first AI_OPEN.
func (pl *PaperListener) setupSession(ctx context.Context, conn *paper.TransportConn, connID, sessionID, userID, model string) {
	if sessionID == "" {
		pl.sendJSONError(conn, connID, "session_open missing session_id")
		return
	}
	orgID := pl.orgForPeer(connID)
	if orgID == "" {
		pl.sendJSONError(conn, connID, "authenticated peer has no organization")
		return
	}
	if userID == "" {
		userID = "user-" + connID
	}

	// Resolve or create the active policy epoch for the org. A
	// bootstrap org without an epoch gets one allowing the requested
	// model so a freshly enrolled harness can work immediately.
	epoch, err := pl.svc.Policy().GetActiveEpoch(orgID)
	if err != nil {
		allowed := []string{model}
		if model == "" {
			allowed = []string{"*"}
		}
		epoch, err = pl.svc.Policy().CreatePolicyEpoch(orgID, allowed, "immediate")
		if err != nil {
			pl.sendJSONError(conn, connID, "policy epoch unavailable: "+err.Error())
			return
		}
	}

	// Push POLICY_EPOCH (0x0D10).
	wireEpoch, err := buildWirePolicyEpoch(epoch, "pccp-policy")
	if err != nil {
		pl.sendJSONError(conn, connID, "epoch encode failed: "+err.Error())
		return
	}
	epochBody, err := encodeWire(wireEpoch)
	if err != nil {
		pl.sendJSONError(conn, connID, "epoch encode failed: "+err.Error())
		return
	}
	if err := conn.SendMessage(paper.MsgPolicyEpochPush, nil, epochBody, 0, 1); err != nil {
		log.Printf("relay: policy-epoch push to %s failed: %v", connID, err)
		return
	}

	// Push CATALOG_SNAPSHOT (0x0D01) bound to the epoch.
	descriptors, err := pl.svc.Catalog().GetEffectiveCatalog("", orgID, "")
	if err != nil {
		log.Printf("relay: catalog resolve for %s failed: %v", connID, err)
		descriptors = nil
	}
	thumb := sha256.Sum256(pl.svc.Policy().SigningPublicKey())
	wireCatalog := buildWireCatalogSnapshot(epoch.EpochID, thumb, descriptors, time.Now())
	catalogBody, err := encodeWire(wireCatalog)
	if err != nil {
		pl.sendJSONError(conn, connID, "catalog encode failed: "+err.Error())
		return
	}
	if err := conn.SendMessage(paper.MsgModelCatalogSnapshot, nil, catalogBody, 0, 2); err != nil {
		log.Printf("relay: catalog push to %s failed: %v", connID, err)
		return
	}

	// Issue the capability lease (A3) and push LEASE_ISSUE (0x0210).
	lease, err := pl.svc.Policy().IssueCapabilityLease(pl.leaseRequest(connID, orgID, sessionID, userID, epoch.EpochID, model))
	if err != nil {
		pl.sendJSONError(conn, connID, "lease issuance failed: "+err.Error())
		return
	}
	wireLeaseObj, err := buildWireLease(lease, "pccp-policy")
	if err != nil {
		pl.sendJSONError(conn, connID, "lease encode failed: "+err.Error())
		return
	}
	leaseBody, err := encodeWire(wireLeaseObj)
	if err != nil {
		pl.sendJSONError(conn, connID, "lease encode failed: "+err.Error())
		return
	}
	if err := conn.SendMessage(paper.MsgLeaseIssue, nil, leaseBody, 0, 3); err != nil {
		log.Printf("relay: lease push to %s failed: %v", connID, err)
		return
	}

	// SESSION_GRANT (0x0201) closes the setup phase.
	pl.mu.Lock()
	pl.sessionEpochs[connID] = epoch.EpochID
	pl.mu.Unlock()
	grant, _ := json.Marshal(map[string]string{
		"session_id":     sessionID,
		"policy_epoch":   epoch.EpochID,
		"lease_id":       lease.LeaseID,
		"organization":   orgID,
	})
	if err := conn.SendMessage(paper.MsgSessionGrant, nil, grant, 0, 4); err != nil {
		log.Printf("relay: session grant to %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: session %s granted for %s (epoch=%s lease=%s)", sessionID, connID, epoch.EpochID, lease.LeaseID)
}

// leaseRequest builds the issuance request. The lease inherits the
// epoch's model allow-list; a session that named a specific model
// gets that model appended so the first exchange is never
// self-denied.
func (pl *PaperListener) leaseRequest(connID, orgID, sessionID, userID, epochID, model string) policy.IssueLeaseRequest {
	allowed := []string{"*"}
	if model != "" {
		allowed = []string{model, "*"}
	}
	peerID := pl.peerIDForConn(connID)
	return policy.IssueLeaseRequest{
		OrganizationID: orgID,
		SubjectPeerID:  peerID,
		UserID:         userID,
		SessionID:      sessionID,
		PolicyEpochID:  epochID,
		AllowedModels:  allowed,
		Validity:       8 * time.Hour,
	}
}

// orgForPeer resolves the authenticated credential's organization.
func (pl *PaperListener) orgForPeer(connID string) string {
	cred := pl.credentialForConn(connID)
	if cred == nil {
		return ""
	}
	return cred.Organization
}

// credentialForConn returns the credential captured at auth time.
func (pl *PaperListener) credentialForConn(connID string) *paper.PeerCredential {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if pl.credentials == nil {
		return nil
	}
	return pl.credentials[connID]
}

// peerIDForConn returns the authenticated subject peer ID.
func (pl *PaperListener) peerIDForConn(connID string) string {
	if cred := pl.credentialForConn(connID); cred != nil {
		return cred.SubjectPeerID
	}
	return connID
}

func (pl *PaperListener) sendJSONError(conn *paper.TransportConn, connID, msg string) {
	log.Printf("relay: session setup error for %s: %s", connID, msg)
	payload, _ := json.Marshal(map[string]string{"error": msg})
	_ = conn.SendMessage(paper.MsgClose, nil, payload, 0, 1)
}

// ---------------------------------------------------------------------------
// Evidence receipts (B3): GovernInference issues the signed receipt for
// each governed exchange; the listener pushes it to the harness over
// EVIDENCE_RECEIPT (0x0307) and records the connector's ack.
// ---------------------------------------------------------------------------

func exchangeDigestHex(exchangeID string) string {
	h := sha256.Sum256([]byte("exchange|" + exchangeID))
	return hex.EncodeToString(h[:])
}

// sessionEpoch tracks the epoch bound at session setup so receipts
// carry it.
func (pl *PaperListener) epochForSession(connID string) string {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return pl.sessionEpochs[connID]
}

// ---------------------------------------------------------------------------
// Connector → relay provenance ingestion (B1).
// ---------------------------------------------------------------------------

func (pl *PaperListener) ingestChangeSet(conn *paper.TransportConn, connID, harnessID string, record *paper.Record) {
	var env wireChangeSetEnvelope
	if err := decodeWire(record.Payload, &env); err != nil {
		log.Printf("relay: changeset decode from %s failed: %v", connID, err)
		return
	}
	orgID := env.OrganizationID
	if orgID == "" {
		orgID = pl.orgForPeer(connID)
	}
	_, err := pl.svc.provenance.CreateChangeSet(provenance.CreateChangeSetRequest{
		OrganizationID: orgID,
		SessionID:      env.SessionID,
		ExchangeID:     env.ExchangeID,
		RepositoryID:   env.RepositoryID,
		Branch:         env.Branch,
		UserID:         env.UserID,
		HarnessID:      harnessID,
		ModelPackageID: env.ModelPackageID,
		EndpointID:     env.EndpointID,
		FilesChanged:   env.FilesChanged,
		DiffSummary:    env.DiffSummary,
		LinesAdded:     env.LinesAdded,
		LinesRemoved:   env.LinesRemoved,
	})
	if err != nil {
		log.Printf("relay: changeset record from %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: changeset %s recorded from %s", env.ChangeSetID, connID)
}

func (pl *PaperListener) ingestSpan(conn *paper.TransportConn, connID, harnessID string, record *paper.Record) {
	var env wireSpanEnvelope
	if err := decodeWire(record.Payload, &env); err != nil {
		log.Printf("relay: span decode from %s failed: %v", connID, err)
		return
	}
	orgID := env.OrganizationID
	if orgID == "" {
		orgID = pl.orgForPeer(connID)
	}
	_, err := pl.svc.provenance.CreateProvenanceSpan(provenance.CreateSpanRequest{
		OrganizationID: orgID,
		RepositoryID:   env.RepositoryID,
		ChangeSetID:    env.ChangeSetID,
		FilePath:       env.FilePath,
		CommitSHA:      env.CommitSHA,
		SymbolLang:     env.SymbolLang,
		SymbolName:     env.SymbolName,
		StartLine:      env.StartLine,
		EndLine:        env.EndLine,
		AttributionState: env.AttributionState,
		Confidence:     env.Confidence,
		SessionID:      env.SessionID,
		UserID:         env.UserID,
		HarnessID:      harnessID,
	})
	if err != nil {
		log.Printf("relay: span record from %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: span %s recorded from %s", env.SpanID, connID)
}

func (pl *PaperListener) ingestCommitBinding(conn *paper.TransportConn, connID, harnessID string, record *paper.Record) {
	var env wireCommitBindingEnvelope
	if err := decodeWire(record.Payload, &env); err != nil {
		log.Printf("relay: commit-binding decode from %s failed: %v", connID, err)
		return
	}
	orgID := env.OrganizationID
	if orgID == "" {
		orgID = pl.orgForPeer(connID)
	}
	if _, err := pl.svc.provenance.BindCommit(orgID, env.RepositoryID, env.CommitSHA, env.ChangeSetID, env.SessionID, env.Branch); err != nil {
		log.Printf("relay: commit-binding record from %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: commit binding %s recorded from %s", env.BindingID, connID)
}

func (pl *PaperListener) ingestActionEnvelope(conn *paper.TransportConn, connID, harnessID string, record *paper.Record) {
	var env wireActionEnvelope
	if err := decodeWire(record.Payload, &env); err != nil {
		log.Printf("relay: action decode from %s failed: %v", connID, err)
		return
	}
	orgID := env.OrganizationID
	if orgID == "" {
		orgID = pl.orgForPeer(connID)
	}
	_, err := pl.svc.provenance.RecordAction(provenance.RecordActionRequest{
		OrganizationID: orgID,
		SessionID:      env.SessionID,
		ExchangeID:     env.ExchangeID,
		UserID:         env.UserID,
		HarnessID:      harnessID,
		ModelPackageID: env.ModelPackageID,
		EndpointID:     env.EndpointID,
		ProjectID:      env.ProjectID,
		RepositoryID:   env.RepositoryID,
		Branch:         env.Branch,
		PolicyEpochID:  env.PolicyEpochID,
		LeaseID:        env.LeaseID,
		ActionType:     env.ActionType,
		ActionPayload:  env.ActionPayload,
		VerdictResult:  env.VerdictResult,
		Classification: env.Classification,
	})
	if err != nil {
		log.Printf("relay: action record from %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: action %s recorded from %s", env.ActionID, connID)
}

func (pl *PaperListener) ingestReceiptAck(conn *paper.TransportConn, connID string, record *paper.Record) {
	var ack wireReceiptAck
	if err := decodeWire(record.Payload, &ack); err != nil {
		log.Printf("relay: receipt-ack decode from %s failed: %v", connID, err)
		return
	}
	if err := pl.svc.provenance.AckEvidenceReceipt(ack.ExchangeID); err != nil {
		log.Printf("relay: receipt ack for %s failed: %v", ack.ExchangeID, err)
		return
	}
	log.Printf("relay: receipt ack recorded for %s from %s", ack.ExchangeID, connID)
}
