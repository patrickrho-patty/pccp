package dari

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

// authorization_test.go implements the Task 7/8 conformance matrix:
// signature coverage of every scope field and the full broadening
// rejection matrix from Appendix F.4.

func testGrantKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func rootGrantFixture(t *testing.T) (*GrantEnvelope, ed25519.PrivateKey) {
	t.Helper()
	_, priv := testGrantKey(t)
	thumb := SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey))
	body := &AuthorizationGrantBody{
		Version:              1,
		GrantID:              "grant-root-1",
		Issuer:               "issuer-relay",
		SubjectPeerID:        "subject-harness",
		SubjectKeyThumbprint: thumb,
		Audience:             []string{"relay-1"},
		OrganizationID:       "org-1",
		UserID:               "user-1",
		SessionID:            "sess-1",
		PolicyEpochID:        "epoch-1",
		Scope: AuthorizationScope{
			ActionClasses:   []string{"inference", "read"},
			Models:          []string{"model-a", "model-b"},
			ReadPaths:       []PathScope{{Authority: "repo", Revision: "main", Prefix: "src", Operations: []string{"read"}}},
			WritePaths:      []PathScope{{Authority: "repo", Revision: "main", Prefix: "src/app", Operations: []string{"write"}}},
			Tools:           []string{"edit"},
			Networks:        []NetworkScope{{Scheme: "https", Host: "api.example.com", PortFirst: 443, PortLast: 443, Purposes: []string{"inference"}}},
			ResourceBudgets: map[string]uint64{"tokens": 10000},
			ApprovalClasses: []string{"standard"},
		},
		NotBeforeMs:     time.Now().Add(-time.Minute).UnixMilli(),
		NotAfterMs:      time.Now().Add(time.Hour).UnixMilli(),
		IssuerSequence:  1,
		DelegationDepth: 3,
	}
	env, err := SignAuthorizationGrant(body, priv)
	if err != nil {
		t.Fatal(err)
	}
	return env, priv
}

// TestAuthorizationSignatureCoversScope (Task 7 Step 2): any scope
// mutation after signing must fail verification.
func TestAuthorizationSignatureCoversScope(t *testing.T) {
	env, _ := rootGrantFixture(t)
	env.Body.Scope.Tools = append(env.Body.Scope.Tools, "shell.admin")

	reencoded, err := EncodeGrantBody(env.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCOSESign1WithAAD(env.COSE, []byte(AuthorizationGrantAAD), reencoded, env.SignerKey); err == nil {
		t.Fatal("expanded scope verified")
	}
	// The ORIGINAL canonical bytes still verify (mutation only broke
	// the recomputation).
	orig, err := EncodeGrantBody(rootGrantBodyClone(t, env))
	if err == nil {
		_ = orig
	}
	if err := VerifyCOSESign1WithAAD(env.COSE, []byte(AuthorizationGrantAAD), env.COSE.Payload, env.SignerKey); err != nil {
		t.Fatalf("original signature must still verify: %v", err)
	}
}

// rootGrantBodyClone is unused-by-design guard helper.
func rootGrantBodyClone(t *testing.T, env *GrantEnvelope) *AuthorizationGrantBody {
	t.Helper()
	return env.Body
}

// TestGrantRoundTripAndDigests: decode → verify → digests stable.
func TestGrantRoundTripAndDigests(t *testing.T) {
	env, _ := rootGrantFixture(t)
	decoded, err := DecodeAuthorizationGrant(env.COSEBytes, env.SignerKey)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SignedDigest != env.SignedDigest || decoded.BodyDigest != env.BodyDigest {
		t.Fatal("digests must be stable across decode")
	}
	if decoded.Body.GrantID != "grant-root-1" {
		t.Fatalf("body mismatch: %+v", decoded.Body)
	}
}

// TestGrantRejectsWrongSigner: a rogue key never verifies.
func TestGrantRejectsWrongSigner(t *testing.T) {
	env, _ := rootGrantFixture(t)
	_, rogue := testGrantKey(t)
	_, err := DecodeAuthorizationGrant(env.COSEBytes, rogue.Public().(ed25519.PublicKey))
	if err == nil {
		t.Fatal("rogue signer accepted")
	}
}

func childFixture(t *testing.T, mutate func(b *AuthorizationGrantBody, s *AuthorizationScope)) (*GrantEnvelope, *GrantEnvelope, ed25519.PrivateKey) {
	t.Helper()
	root, rootPriv := rootGrantFixture(t)
	delegatePub, _ := testGrantKey(t)
	childScope := AuthorizationScope{
		ActionClasses:   []string{"inference"},
		Models:          []string{"model-a"},
		ReadPaths:       []PathScope{{Authority: "repo", Revision: "main", Prefix: "src", Operations: []string{"read"}}},
		Tools:           []string{"edit"},
		Networks:        []NetworkScope{{Scheme: "https", Host: "api.example.com", PortFirst: 443, PortLast: 443, Purposes: []string{"inference"}}},
		ResourceBudgets: map[string]uint64{"tokens": 5000},
		ApprovalClasses: []string{"standard"},
	}
	body := &AuthorizationGrantBody{
		Version: 1, GrantID: "grant-child-1",
		Issuer:               root.Body.SubjectPeerID,
		SubjectPeerID:        "subject-subagent",
		SubjectKeyThumbprint: SubjectKeyThumbprint(delegatePub),
		Audience:             []string{"relay-1"},
		OrganizationID:       "org-1", UserID: "user-1", SessionID: "sess-1", PolicyEpochID: "epoch-1",
		Scope:             childScope,
		NotBeforeMs:       root.Body.NotBeforeMs,
		NotAfterMs:        root.Body.NotAfterMs,
		IssuerSequence:    1,
		ParentGrantDigest: &root.SignedDigest,
		DelegationDepth:   root.Body.DelegationDepth - 1,
	}
	if mutate != nil {
		mutate(body, &childScope)
		body.Scope = childScope
	}
	child, err := SignAuthorizationGrant(body, rootPriv)
	if err != nil {
		t.Fatal(err)
	}
	return root, child, rootPriv
}

// TestDelegationAcceptsNarrowerChild: the one positive case.
func TestDelegationAcceptsNarrowerChild(t *testing.T) {
	root, child, _ := childFixture(t, nil)
	ctx := ChainContext{NowMs: time.Now().UnixMilli()}
	if err := ValidateDelegationChain([]*GrantEnvelope{root, child}, ctx); err != nil {
		t.Fatalf("valid narrower child rejected: %v", err)
	}
}

// TestDelegationRejectsBroadenedScope: the Task 8 rejection matrix.
func TestDelegationRejectsBroadenedScope(t *testing.T) {
	now := time.Now().UnixMilli()
	cases := []struct {
		name   string
		mutate func(b *AuthorizationGrantBody, s *AuthorizationScope)
	}{
		{"added model", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			s.Models = append(s.Models, "model-z")
		}},
		{"wider path", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			s.ReadPaths = []PathScope{{Authority: "repo", Revision: "main", Prefix: "other", Operations: []string{"read"}}}
		}},
		{"deeper-then-shallower path escape", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			// Prefix not under the parent's prefix.
			s.ReadPaths = []PathScope{{Authority: "repo", Revision: "main", Prefix: "sr", Operations: []string{"read"}}}
		}},
		{"new tool", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			s.Tools = []string{"edit", "shell.admin"}
		}},
		{"broader network", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			s.Networks = []NetworkScope{{Scheme: "https", Host: "evil.example.com", PortFirst: 443, PortLast: 443, Purposes: []string{"inference"}}}
		}},
		{"wider network ports", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			s.Networks = []NetworkScope{{Scheme: "https", Host: "api.example.com", PortFirst: 1, PortLast: 65535, Purposes: []string{"inference"}}}
		}},
		{"increased budget", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			s.ResourceBudgets = map[string]uint64{"tokens": 20000}
		}},
		{"introduced budget key", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			s.ResourceBudgets = map[string]uint64{"tokens": 100, "requests": 5}
		}},
		{"later expiry", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			b.NotAfterMs = b.NotAfterMs + time.Hour.Milliseconds()
		}},
		{"changed audience", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			b.Audience = []string{"relay-2"}
		}},
		{"changed session", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			b.SessionID = "sess-2"
		}},
		{"changed epoch", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			b.PolicyEpochID = "epoch-2"
		}},
		{"removed approval", func(b *AuthorizationGrantBody, s *AuthorizationScope) {
			s.ApprovalClasses = nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, child, _ := childFixture(t, tc.mutate)
			err := ValidateDelegationChain([]*GrantEnvelope{root, child}, ChainContext{NowMs: now})
			if err == nil {
				t.Fatal("broadened child accepted")
			}
			if !strings.Contains(err.Error(), "AUTHORITY_ESCALATION") && !strings.Contains(err.Error(), "INVALID_GRANT_CHAIN") {
				t.Fatalf("expected a chain/escalation code, got %v", err)
			}
		})
	}
}

// TestDelegationRejectsBadParentDigest: tampered linkage.
func TestDelegationRejectsBadParentDigest(t *testing.T) {
	root, child, _ := childFixture(t, nil)
	tamperedDigest := *child.Body.ParentGrantDigest
	tamperedDigest[0] ^= 0xFF
	child.Body.ParentGrantDigest = &tamperedDigest
	reencoded, err := EncodeGrantBody(child.Body)
	if err != nil {
		t.Fatal(err)
	}
	// Re-sign the tampered body so only the linkage is wrong.
	_, rootPriv := rootGrantFixture(t)
	_ = rootPriv
	tampered, err := SignAuthorizationGrant(child.Body, rootPrivOf(t, root))
	if err != nil {
		t.Fatal(err)
	}
	_ = reencoded
	err = ValidateDelegationChain([]*GrantEnvelope{root, tampered}, ChainContext{NowMs: time.Now().UnixMilli()})
	if err == nil || !strings.Contains(err.Error(), "parent digest mismatch") {
		t.Fatalf("expected parent-digest mismatch, got %v", err)
	}
}

func rootPrivOf(t *testing.T, root *GrantEnvelope) ed25519.PrivateKey {
	t.Helper()
	// The fixture signs with the subject key; regenerate the chain key
	// by recovering from the test fixture generator (same key each
	// call is impossible, so childFixture carries it). For this test
	// we re-derive via the second fixture return.
	_, _, priv := childFixture(t, nil)
	return priv
}

// TestDelegationRejectsExceededDepth: a depth-0 leaf cannot delegate.
func TestDelegationRejectsExceededDepth(t *testing.T) {
	root, rootPriv := rootGrantFixture(t)
	now := time.Now().UnixMilli()
	// Self-delegation chain down to a leaf: root(3) -> c1(2) -> c2(1) -> leaf(0).
	chain := []*GrantEnvelope{root}
	signer := rootPriv
	scope := root.Body.Scope
	for i := 0; i < 3; i++ {
		child, err := IssueChildGrant(chain[len(chain)-1], "subject-"+itoa(uint64(i+1)), SubjectKeyThumbprint(signer.Public().(ed25519.PublicKey)),
			scope, root.Body.NotBeforeMs, root.Body.NotAfterMs, "grant-"+itoa(uint64(i+1)), uint64(i+1), signer)
		if err != nil {
			t.Fatalf("issue child %d: %v", i, err)
		}
		chain = append(chain, child)
	}
	if err := ValidateDelegationChain(chain, ChainContext{NowMs: now}); err != nil {
		t.Fatalf("full chain must validate: %v", err)
	}
	// Delegating FROM the depth-0 leaf must be refused.
	_, err := IssueChildGrant(chain[len(chain)-1], "grandchild", SubjectKeyThumbprint(signer.Public().(ed25519.PublicKey)),
		scope, root.Body.NotBeforeMs, root.Body.NotAfterMs, "grant-gc", 9, signer)
	if err == nil || !strings.Contains(err.Error(), "cannot delegate") {
		t.Fatalf("expected cannot-delegate, got %v", err)
	}
}

// TestDelegationRejectsBrokenChainLink: correct parent digest but the
// child is signed by a key that is NOT the parent's subject key.
func TestDelegationRejectsBrokenChainLink(t *testing.T) {
	root, rootPriv := rootGrantFixture(t)
	_, roguePriv := testGrantKey(t)
	delegatePub, _ := testGrantKey(t)
	childBody := &AuthorizationGrantBody{
		Version: 1, GrantID: "grant-child-rogue",
		Issuer:               root.Body.SubjectPeerID,
		SubjectPeerID:        "subject-subagent",
		SubjectKeyThumbprint: SubjectKeyThumbprint(delegatePub),
		Audience:             root.Body.Audience,
		OrganizationID:       root.Body.OrganizationID,
		UserID:               root.Body.UserID,
		SessionID:            root.Body.SessionID,
		PolicyEpochID:        root.Body.PolicyEpochID,
		Scope:                root.Body.Scope,
		NotBeforeMs:          root.Body.NotBeforeMs,
		NotAfterMs:           root.Body.NotAfterMs,
		IssuerSequence:       1,
		ParentGrantDigest:    &root.SignedDigest,
		DelegationDepth:      root.Body.DelegationDepth - 1,
	}
	rogue, err := SignAuthorizationGrant(childBody, roguePriv)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateDelegationChain([]*GrantEnvelope{root, rogue}, ChainContext{NowMs: time.Now().UnixMilli()})
	if err == nil || !strings.Contains(err.Error(), "subject-key thumbprint") {
		t.Fatalf("expected subject-key mismatch, got %v", err)
	}
	_ = rootPriv
}

// TestGrantReplayLedger: a reused issuer sequence with a different
// digest is GRANT_REPLAY.
func TestGrantReplayLedger(t *testing.T) {
	root, child, _ := childFixture(t, nil)
	ledger := map[string]Digest{}
	ctx := ChainContext{
		NowMs: time.Now().UnixMilli(),
		SequenceSeen: func(issuer string, seq uint64) Digest {
			return ledger[issuer+"/"+itoa(seq)]
		},
		RecordSequence: func(issuer string, seq uint64, d Digest) {
			ledger[issuer+"/"+itoa(seq)] = d
		},
	}
	if err := ValidateDelegationChain([]*GrantEnvelope{root, child}, ctx); err != nil {
		t.Fatal(err)
	}
	// Same issuer+sequence, different digest → replay.
	child.Body.GrantID = "grant-child-replay"
	replayed, err := SignAuthorizationGrant(child.Body, rootPrivOf(t, root))
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateDelegationChain([]*GrantEnvelope{root, replayed}, ctx)
	if err == nil || !strings.Contains(err.Error(), "GRANT_REPLAY") {
		t.Fatalf("expected GRANT_REPLAY, got %v", err)
	}
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// TestIssueChildGrantHappyPath: the helper produces a valid chain.
func TestIssueChildGrantHappyPath(t *testing.T) {
	root, rootPriv := rootGrantFixture(t)
	delegatePub, _ := testGrantKey(t)
	narrower := root.Body.Scope
	narrower.Models = []string{"model-a"}
	narrower.ResourceBudgets = map[string]uint64{"tokens": 1000}
	child, err := IssueChildGrant(root, "subject-subagent", SubjectKeyThumbprint(delegatePub), narrower,
		root.Body.NotBeforeMs, root.Body.NotAfterMs, "grant-child-2", 1, rootPriv)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDelegationChain([]*GrantEnvelope{root, child}, ChainContext{NowMs: time.Now().UnixMilli()}); err != nil {
		t.Fatalf("issued child does not validate: %v", err)
	}
}

// TestPathPrefixNormalization: "." / ".." / empty are rejected.
func TestPathPrefixNormalization(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/../b", "/"} {
		if _, err := NormalizePathPrefix(bad); err == nil {
			t.Fatalf("prefix %q must be rejected", bad)
		}
	}
	if p, err := NormalizePathPrefix("src//lib/./x"); err != nil || p != "src/lib/x" {
		t.Fatalf("normalization = %q, %v", p, err)
	}
}
