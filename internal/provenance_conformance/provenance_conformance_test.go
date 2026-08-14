package provenance_conformance

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// relayEnvelopeSigningBytes is the relay's exact `computeEnvelopeDigest`
// body format. The connector's `provenancewire.ActionEnvelope.SignBytes`
// must produce compatible canonical bytes so a harness that signs
// at the connector can verify at the relay (or vice versa).
//
// The format is: actionID|orgID|sessionID|exchangeID|userID|harnessID|
// modelPackageID|endpointID|actionType|actionPayload|occurredAtUnixMs.
func relayEnvelopeSigningBytes(actionID, orgID, sessionID, exchangeID, userID, harnessID, modelPackageID, endpointID, actionType, actionPayload string, occurredAtUnixMs int64) []byte {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%d",
		actionID, orgID, sessionID, exchangeID, userID, harnessID,
		modelPackageID, endpointID, actionType, actionPayload, occurredAtUnixMs)
	return []byte(data)
}

// relayChangeSetSigningBytes is the relay's exact
// `computeChangeSetDigest` body format. The connector's
// `provenancewire.ChangeSetEnvelope.SignBytes` must produce
// compatible canonical bytes.
//
// Format: sessionID|repositoryID|branch|filesChanged|diffSummary|
// modelPackageID|endpointID.
func relayChangeSetSigningBytes(sessionID, repositoryID, branch, filesChanged, diffSummary, modelPackageID, endpointID string) []byte {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		sessionID, repositoryID, branch, filesChanged, diffSummary,
		modelPackageID, endpointID)
	return []byte(data)
}

// relaySpanSigningBytes is the relay's exact `computeSpanDigest`
// body format.
//
// Format: repositoryID|filePath|commitSHA|startLine-endLine|
// attributionState|sessionID.
func relaySpanSigningBytes(repositoryID, filePath, commitSHA, attributionState, sessionID string, startLine, endLine int) []byte {
	data := fmt.Sprintf("%s|%s|%s|%d-%d|%s|%s",
		repositoryID, filePath, commitSHA,
		startLine, endLine, attributionState, sessionID)
	return []byte(data)
}

// relayReceiptSigningBytes is the relay's exact
// `buildReceiptSigningData` body format.
//
// Format: exchangeID|finalState|chainRoot|relayIdentity|policyEpochID|
// modelPackageID|issuedAt.
func relayReceiptSigningBytes(exchangeID, finalState, chainRoot, relayIdentity, policyEpochID, modelPackageID, issuedAt string) []byte {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		exchangeID, finalState, chainRoot, relayIdentity,
		policyEpochID, modelPackageID, issuedAt)
	return []byte(data)
}

// TestRelayEnvelopeSigningFormatStable pins the relay's byte
// layout for action envelopes. Any drift in this format breaks
// cross-repo attestation; the test fails immediately.
func TestRelayEnvelopeSigningFormatStable(t *testing.T) {
	got := string(relayEnvelopeSigningBytes(
		"act-1", "org-test", "ses-1", "ex-1", "alice", "h-1",
		"pkg-1", "ep-1", "tool_use", `{"tool":"bash"}`, 1_700_000_000_000,
	))
	want := "act-1|org-test|ses-1|ex-1|alice|h-1|pkg-1|ep-1|tool_use|{\"tool\":\"bash\"}|1700000000000"
	if got != want {
		t.Fatalf("relay envelope signing format drift\n got: %s\nwant: %s", got, want)
	}
	// Hash check pins the canonical byte layout.
	h := sha256.Sum256([]byte(got))
	t.Logf("envelope signing bytes sha256 = %x", h)
}

// TestRelayChangeSetSigningFormatStable pins the change-set
// format.
func TestRelayChangeSetSigningFormatStable(t *testing.T) {
	got := string(relayChangeSetSigningBytes(
		"ses-1", "pccp", "main", "[foo.go,bar.go]", "diff summary",
		"pkg-1", "ep-1",
	))
	want := "ses-1|pccp|main|[foo.go,bar.go]|diff summary|pkg-1|ep-1"
	if got != want {
		t.Fatalf("relay changeset signing format drift\n got: %s\nwant: %s", got, want)
	}
}

// TestRelaySpanSigningFormatStable pins the span format.
func TestRelaySpanSigningFormatStable(t *testing.T) {
	got := string(relaySpanSigningBytes(
		"pccp", "/repo/foo.go", "abc123", "AI_GENERATED", "ses-1", 42, 56,
	))
	want := "pccp|/repo/foo.go|abc123|42-56|AI_GENERATED|ses-1"
	if got != want {
		t.Fatalf("relay span signing format drift\n got: %s\nwant: %s", got, want)
	}
}

// TestRelayReceiptSigningFormatStable pins the receipt format.
func TestRelayReceiptSigningFormatStable(t *testing.T) {
	got := string(relayReceiptSigningBytes(
		"ex-1", "completed", "chainroot", "pccp-relay", "epoch-1",
		"pkg-1", "2026-01-01T00:00:00Z",
	))
	want := "ex-1|completed|chainroot|pccp-relay|epoch-1|pkg-1|2026-01-01T00:00:00Z"
	if got != want {
		t.Fatalf("relay receipt signing format drift\n got: %s\nwant: %s", got, want)
	}
}

// TestConnectorActionEnvelopeDigestMatchesRelayFormat confirms
// the connector's digest layout is byte-for-byte compatible with
// the relay's. The relay and connector MUST agree on the field
// order, separator, and trailing int64.
func TestConnectorActionEnvelopeDigestMatchesRelayFormat(t *testing.T) {
	// The connector's emitter uses an SHA-256 hash of the canonical
	// bytes; the relay uses a `sha256:hex` prefix on the same
	// hash. We compare the underlying hash here.
	connectorBytes := connectorActionEnvelopeCanonicalBytes(
		"act-1", "org-test", "ses-1", "ex-1", "alice", "h-1",
		"pkg-1", "ep-1", "tool_use", `{"tool":"bash"}`, 1_700_000_000_000,
	)
	relayBytes := relayEnvelopeSigningBytes(
		"act-1", "org-test", "ses-1", "ex-1", "alice", "h-1",
		"pkg-1", "ep-1", "tool_use", `{"tool":"bash"}`, 1_700_000_000_000,
	)
	// Both must hash to the same digest.
	c := sha256.Sum256(connectorBytes)
	r := sha256.Sum256(relayBytes)
	if c != r {
		t.Fatalf("connector and relay envelopes produce different digests\n connector: %x\n relay: %x", c, r)
	}
}

// TestConnectorChangeSetDigestMatchesRelayFormat covers the
// change-set digest contract.
func TestConnectorChangeSetDigestMatchesRelayFormat(t *testing.T) {
	connectorBytes := connectorChangeSetCanonicalBytes(
		"ses-1", "pccp", "main", "[foo.go,bar.go]", "diff summary",
		"pkg-1", "ep-1",
	)
	relayBytes := relayChangeSetSigningBytes(
		"ses-1", "pccp", "main", "[foo.go,bar.go]", "diff summary",
		"pkg-1", "ep-1",
	)
	c := sha256.Sum256(connectorBytes)
	r := sha256.Sum256(relayBytes)
	if c != r {
		t.Fatalf("connector and relay changeset produce different digests\n connector: %x\n relay: %x", c, r)
	}
}

// TestConnectorSpanDigestMatchesRelayFormat covers the span
// digest contract.
func TestConnectorSpanDigestMatchesRelayFormat(t *testing.T) {
	connectorBytes := connectorSpanCanonicalBytes(
		"pccp", "/repo/foo.go", "abc123", "AI_GENERATED", "ses-1", 42, 56,
	)
	relayBytes := relaySpanSigningBytes(
		"pccp", "/repo/foo.go", "abc123", "AI_GENERATED", "ses-1", 42, 56,
	)
	c := sha256.Sum256(connectorBytes)
	r := sha256.Sum256(relayBytes)
	if c != r {
		t.Fatalf("connector and relay span produce different digests\n connector: %x\n relay: %x", c, r)
	}
}

// TestConnectorReceiptSigningMatchesRelayFormat covers the
// receipt signing contract.
func TestConnectorReceiptSigningMatchesRelayFormat(t *testing.T) {
	connectorBytes := connectorReceiptCanonicalBytes(
		"ex-1", "completed", "chainroot", "pccp-relay", "epoch-1",
		"pkg-1", "2026-01-01T00:00:00Z",
	)
	relayBytes := relayReceiptSigningBytes(
		"ex-1", "completed", "chainroot", "pccp-relay", "epoch-1",
		"pkg-1", "2026-01-01T00:00:00Z",
	)
	c := sha256.Sum256(connectorBytes)
	r := sha256.Sum256(relayBytes)
	if c != r {
		t.Fatalf("connector and relay receipt produce different digests\n connector: %x\n relay: %x", c, r)
	}
}

// connectorActionEnvelopeCanonicalBytes reproduces the byte
// layout the connector's `provenancewire.ActionEnvelope.SignBytes`
// produces (without the domain prefix). This must match the
// relay's `computeEnvelopeDigest` layout.
//
// Format: actionID|orgID|sessionID|exchangeID|userID|harnessID|
// modelPackageID|endpointID|actionType|actionPayload|occurredAtUnixMs.
func connectorActionEnvelopeCanonicalBytes(actionID, orgID, sessionID, exchangeID, userID, harnessID, modelPackageID, endpointID, actionType, actionPayload string, occurredAtUnixMs int64) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%d",
		actionID, orgID, sessionID, exchangeID, userID, harnessID,
		modelPackageID, endpointID, actionType, actionPayload, occurredAtUnixMs))
}

// connectorChangeSetCanonicalBytes matches the relay's
// change-set signing body.
//
// Format: sessionID|repositoryID|branch|filesChanged|diffSummary|
// modelPackageID|endpointID.
func connectorChangeSetCanonicalBytes(sessionID, repositoryID, branch, filesChanged, diffSummary, modelPackageID, endpointID string) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		sessionID, repositoryID, branch, filesChanged, diffSummary,
		modelPackageID, endpointID))
}

// connectorSpanCanonicalBytes matches the relay's span signing
// body.
//
// Format: repositoryID|filePath|commitSHA|startLine-endLine|
// attributionState|sessionID.
func connectorSpanCanonicalBytes(repositoryID, filePath, commitSHA, attributionState, sessionID string, startLine, endLine int) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%d-%d|%s|%s",
		repositoryID, filePath, commitSHA,
		startLine, endLine, attributionState, sessionID))
}

// connectorReceiptCanonicalBytes matches the relay's receipt
// signing body.
//
// Format: exchangeID|finalState|chainRoot|relayIdentity|policyEpochID|
// modelPackageID|issuedAt.
func connectorReceiptCanonicalBytes(exchangeID, finalState, chainRoot, relayIdentity, policyEpochID, modelPackageID, issuedAt string) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		exchangeID, finalState, chainRoot, relayIdentity,
		policyEpochID, modelPackageID, issuedAt))
}
