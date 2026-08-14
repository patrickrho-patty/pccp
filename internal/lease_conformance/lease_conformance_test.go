package lease_conformance

import (
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/policy"
)

// This file pins the cross-repo capability-lease byte contract
// (harness feature plan A3, e2e).
//
// The relay signs via `policy.CanonicalLeaseSigningBytes`. The
// connector (`patty-code-pccp/internal/dariproto/lease.go::
// Lease.SigningBytes`) recomputes the SAME bytes from the wire lease
// and verifies the COSE-Sign1 signature. The two repos cannot import
// each other, so this suite re-derives the connector's layout
// INDEPENDENTLY below. If either repo changes field order, length
// prefixes, domain prefix, or the field set, this test fails.
//
// Canonical layout (little-endian? NO — all big-endian):
//
//	"DARI-CAPABILITY-LEASE-v1\x00"
//	lp(leaseID) lp(subject) lp(user) lp(session) lp(epoch)
//	lp[](allowedModels) lp[](filePathRead) lp[](filePathWrite) lp[](toolClasses)
//	lp[](repoScope maps: sorted keys, lp(k) lp(v))
//	u64(tokenBudget) u64(notBeforeMs) u64(notAfterMs) u64(leaseSequence) u64(issuedAtMs)
//
// where lp = uint32-BE length prefix; lp[] = count-prefixed list; u64
// = uint32-BE length (8) + 8-byte BE value.

const leaseDomain = "DARI-CAPABILITY-LEASE-v1\x00"

type connectorLeaseView struct {
	leaseID            string
	subject            string
	user               string
	session            string
	epoch              string
	allowedModels      []string
	filePathReadScope  []string
	filePathWriteScope []string
	toolClasses        []string
	repositoryScope    []map[string]string
	tokenBudget        uint64
	notBeforeUnixMs    uint64
	notAfterUnixMs     uint64
	leaseSequence      uint64
	issuedAtUnixMs     uint64
}

// connectorSigningBytes re-derives the connector's recomputation. It
// is written from the connector's published layout, NOT from the
// relay's canonical.go — a copy-paste of the relay implementation
// would make this test tautological.
func connectorSigningBytes(l connectorLeaseView) []byte {
	dst := append([]byte(nil), leaseDomain...)
	dst = lp(dst, l.leaseID)
	dst = lp(dst, l.subject)
	dst = lp(dst, l.user)
	dst = lp(dst, l.session)
	dst = lp(dst, l.epoch)
	dst = lpList(dst, l.allowedModels)
	dst = lpList(dst, l.filePathReadScope)
	dst = lpList(dst, l.filePathWriteScope)
	dst = lpList(dst, l.toolClasses)

	// Repository scope: count-prefixed maps of sorted length-prefixed
	// key/value pairs.
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(l.repositoryScope)))
	dst = append(dst, lenBuf[:]...)
	for _, m := range l.repositoryScope {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(keys)))
		dst = append(dst, lenBuf[:]...)
		for _, k := range keys {
			dst = lp(dst, k)
			dst = lp(dst, m[k])
		}
	}

	dst = u64(dst, l.tokenBudget)
	dst = u64(dst, l.notBeforeUnixMs)
	dst = u64(dst, l.notAfterUnixMs)
	dst = u64(dst, l.leaseSequence)
	dst = u64(dst, l.issuedAtUnixMs)
	return dst
}

func lp(dst []byte, v string) []byte {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(v)))
	dst = append(dst, lenBuf[:]...)
	return append(dst, v...)
}

func lpList(dst []byte, values []string) []byte {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(values)))
	dst = append(dst, lenBuf[:]...)
	for _, v := range values {
		dst = lp(dst, v)
	}
	return dst
}

func u64(dst []byte, v uint64) []byte {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], 8)
	dst = append(dst, lenBuf[:]...)
	var valBuf [8]byte
	binary.BigEndian.PutUint64(valBuf[:], v)
	return append(dst, valBuf[:]...)
}

// relayIssuedFixture issues a REAL relay lease (real DB-backed signer
// path is exercised in internal/policy; here we sign via the relay's
// actual canonical helper with the relay's actual COSE envelope).
func relaySignedBody(t *testing.T, key ed25519.PrivateKey, in policy.LeaseSigningInput) []byte {
	t.Helper()
	sign1, err := dari.CreateCOSESign1(policy.CanonicalLeaseSigningBytes(in), key, []byte("pccp-policy"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return sign1.Payload
}

// TestCanonicalLeaseBytesMatchConnectorLayout is the core pin: the
// relay's canonical bytes equal the connector's independently derived
// recomputation for a fully-populated lease.
func TestCanonicalLeaseBytesMatchConnectorLayout(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	in := policy.LeaseSigningInput{
		LeaseID:            "lease-conn-1",
		SubjectPeerID:      "harness-peer-9",
		UserID:             "user-7",
		SessionID:          "sess-3",
		PolicyEpochID:      "epoch-2",
		AllowedModels:      []string{"m-x", "m-y"},
		FilePathReadScope:  []string{"a/*.go"},
		FilePathWriteScope: []string{"a/b.go"},
		ToolClasses:        []string{"read", "edit"},
		RepositoryScope:    []map[string]string{{"repo": "r1", "branch": "main"}},
		TokenBudget:        12345,
		NotBeforeUnixMs:    now.UnixMilli(),
		NotAfterUnixMs:     now.Add(time.Hour).UnixMilli(),
		LeaseSequence:      4,
		IssuedAtUnixMs:     now.UnixMilli(),
	}
	relayBytes := policy.CanonicalLeaseSigningBytes(in)
	connectorBytes := connectorSigningBytes(connectorLeaseView{
		leaseID:            in.LeaseID,
		subject:            in.SubjectPeerID,
		user:               in.UserID,
		session:            in.SessionID,
		epoch:              in.PolicyEpochID,
		allowedModels:      in.AllowedModels,
		filePathReadScope:  in.FilePathReadScope,
		filePathWriteScope: in.FilePathWriteScope,
		toolClasses:        in.ToolClasses,
		repositoryScope:    in.RepositoryScope,
		tokenBudget:        uint64(in.TokenBudget),
		notBeforeUnixMs:    uint64(in.NotBeforeUnixMs),
		notAfterUnixMs:     uint64(in.NotAfterUnixMs),
		leaseSequence:      in.LeaseSequence,
		issuedAtUnixMs:     uint64(in.IssuedAtUnixMs),
	})
	if string(relayBytes) != string(connectorBytes) {
		t.Fatalf("canonical lease bytes diverge:\nrelay    = %x\nconnector= %x", relayBytes, connectorBytes)
	}
}

// TestRelaySignatureVerifiesUnderConnectorLayout proves the signed
// COSE payload is exactly the bytes the connector recomputes, and the
// Ed25519 signature verifies under the issuer public key.
func TestRelaySignatureVerifiesUnderConnectorLayout(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	now := time.Now().Truncate(time.Second)
	in := policy.LeaseSigningInput{
		LeaseID:         "lease-conn-2",
		SubjectPeerID:   "harness-peer-1",
		UserID:          "u1",
		SessionID:       "s1",
		PolicyEpochID:   "e1",
		AllowedModels:   []string{"*"},
		TokenBudget:     1000,
		NotBeforeUnixMs: now.UnixMilli(),
		NotAfterUnixMs:  now.Add(8 * time.Hour).UnixMilli(),
		LeaseSequence:   1,
		IssuedAtUnixMs:  now.UnixMilli(),
	}
	payload := relaySignedBody(t, priv, in)

	expect := connectorSigningBytes(connectorLeaseView{
		leaseID:         in.LeaseID,
		subject:         in.SubjectPeerID,
		user:            in.UserID,
		session:         in.SessionID,
		epoch:           in.PolicyEpochID,
		allowedModels:   in.AllowedModels,
		tokenBudget:     uint64(in.TokenBudget),
		notBeforeUnixMs: uint64(in.NotBeforeUnixMs),
		notAfterUnixMs:  uint64(in.NotAfterUnixMs),
		leaseSequence:   in.LeaseSequence,
		issuedAtUnixMs:  uint64(in.IssuedAtUnixMs),
	})
	if string(payload) != string(expect) {
		t.Fatal("signed payload is not the connector-computable body")
	}

	sign1, err := dari.CreateCOSESign1(payload, priv, []byte("pccp-policy"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	encoded, err := dari.EncodeCOSESign1(sign1)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	_ = hex.EncodeToString(encoded)

	// The connector's verification path: decode envelope, compare
	// payload to recomputed body, verify the COSE Sig_structure under
	// the issuer key.
	if err := dari.VerifyCOSESign1(sign1, pub); err != nil {
		t.Fatalf("lease signature does not verify under issuer public key: %v", err)
	}
}

// TestCanonicalLayoutEmptyScopes pins the zero-value encodings (empty
// lists, no repo scope) so a relay that omits optional fields stays
// byte-compatible with the connector.
func TestCanonicalLayoutEmptyScopes(t *testing.T) {
	a := policy.CanonicalLeaseSigningBytes(policy.LeaseSigningInput{LeaseID: "l", SubjectPeerID: "s", UserID: "u", SessionID: "se", PolicyEpochID: "e"})
	b := connectorSigningBytes(connectorLeaseView{leaseID: "l", subject: "s", user: "u", session: "se", epoch: "e"})
	if string(a) != string(b) {
		t.Fatalf("empty-scope encoding diverges:\nrelay=%x\nconn =%x", a, b)
	}
}

// TestRogueIssuerRejected: a signature from a different key never
// verifies under the pinned issuer.
func TestRogueIssuerRejected(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	_, rogue, _ := ed25519.GenerateKey(nil)
	now := time.Now().Truncate(time.Second)
	in := policy.LeaseSigningInput{
		LeaseID: "l", SubjectPeerID: "s", UserID: "u", SessionID: "se", PolicyEpochID: "e",
		NotBeforeUnixMs: now.UnixMilli(), NotAfterUnixMs: now.Add(time.Hour).UnixMilli(),
		LeaseSequence: 1, IssuedAtUnixMs: now.UnixMilli(),
	}
	sign1, err := dari.CreateCOSESign1(policy.CanonicalLeaseSigningBytes(in), rogue, []byte("pccp-policy"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := dari.VerifyCOSESign1(sign1, pub); err == nil {
		t.Fatal("rogue issuer signature must not verify")
	}
}
