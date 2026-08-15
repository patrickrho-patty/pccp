package dari

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// authorization.go implements the DARI Authorization Grant and its
// signed-object semantics (spec Appendix F.2/F.4, master plan Task 7).
// The grant is the kernel's authority object: every governed exchange,
// decision, and effect binds to a leaf grant's signed-object digest.

// ---------------------------------------------------------------------------
// F.12 kernel object-type values (dari/1 signed objects).
// ---------------------------------------------------------------------------

const (
	ObjTypeAuthorizationGrant    ObjectType = 0x0202
	ObjTypeGovernedExchange      ObjectType = 0x0303
	ObjTypeAuthorizationDecision ObjectType = 0x0304
	ObjTypeSignedStateCheckpoint ObjectType = 0x0305
	ObjTypeEffectPrepare         ObjectType = 0x0610
	ObjTypeEffectAuthorize       ObjectType = 0x0611
	ObjTypeEffectResult          ObjectType = 0x0612
	ObjTypeEffectStatusResp      ObjectType = 0x0613
	ObjTypeReceiptAttestation    ObjectType = 0x0703
	ObjTypeSelectiveDisclosure   ObjectType = 0x0704
)

// KernelObjectDigest is the F.2 object digest for dari/1 kernel
// objects (no reserved byte — distinct from the legacy profile's
// ComputeObjectDigest):
//
//	SHA-256("DARI-OBJ-v1\0" || uint16_be(T) || deterministic_cbor(body))
func KernelObjectDigest(t ObjectType, body []byte) Digest {
	return kernelTypedDigest("DARI-OBJ-v1\x00", t, body)
}

// KernelSignedObjectDigest is the F.2 digest of a signed envelope:
//
//	SHA-256("DARI-SIGNED-OBJ-v1\0" || uint16_be(T) || deterministic_cbor(cose))
func KernelSignedObjectDigest(t ObjectType, cose []byte) Digest {
	return kernelTypedDigest("DARI-SIGNED-OBJ-v1\x00", t, cose)
}

func kernelTypedDigest(domain string, t ObjectType, body []byte) Digest {
	h := sha256.New()
	h.Write([]byte(domain))
	var tb [2]byte
	binary.BigEndian.PutUint16(tb[:], uint16(t))
	h.Write(tb[:])
	h.Write(body)
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}

// ---------------------------------------------------------------------------
// COSE with external AAD (F.2: each kernel object names its own AAD).
// ---------------------------------------------------------------------------

// CreateCOSESign1WithAAD signs a payload with an external AAD per F.2:
// protected = {1: -8, 4: kid} (both protected, nothing else), the
// unprotected map empty, and the Sig_structure array
// ["Signature1", protected, external_aad, payload].
func CreateCOSESign1WithAAD(payload, externalAAD []byte, priv ed25519.PrivateKey, keyID []byte) (*COSESign1, error) {
	if len(priv) == 0 {
		return nil, errors.New("dari: empty private key")
	}
	protected := COSEHeader{Alg: COSEAlgEdDSA, KID: keyID}
	protectedBytes, err := MarshalCBOR(protected)
	if err != nil {
		return nil, fmt.Errorf("dari: marshal protected header: %w", err)
	}
	sigInput := []interface{}{"Signature1", protectedBytes, externalAAD, payload}
	sigBytes, err := MarshalCBOR(sigInput)
	if err != nil {
		return nil, fmt.Errorf("dari: marshal sig structure: %w", err)
	}
	return &COSESign1{
		Protected:   protectedBytes,
		Unprotected: COSEHeader{},
		Payload:     payload,
		Signature:   ed25519.Sign(priv, sigBytes),
	}, nil
}

// VerifyCOSESign1WithAAD verifies an F.2 COSE_Sign1: exact protected
// header shape ({alg, kid} only), empty unprotected map, signature over
// the AAD-bound Sig_structure, and payload equality with the caller's
// expected canonical body bytes.
func VerifyCOSESign1WithAAD(sign1 *COSESign1, externalAAD, expectedPayload []byte, pub ed25519.PublicKey) error {
	if sign1 == nil {
		return errors.New("dari: nil COSE-Sign1")
	}
	if len(pub) == 0 {
		return errors.New("dari: empty public key")
	}
	// Protected header MUST decode to exactly {1: -8, 4: kid}.
	var hdr COSEHeader
	if err := UnmarshalCBOR(sign1.Protected, &hdr); err != nil {
		return fmt.Errorf("dari: decode protected header: %w", err)
	}
	if hdr.Alg != COSEAlgEdDSA || len(hdr.KID) == 0 || len(hdr.Other) != 0 {
		return errors.New("dari: protected header must be exactly {alg:-8, kid}")
	}
	// Unprotected map MUST be empty.
	if sign1.Unprotected.Alg != 0 || sign1.Unprotected.KID != nil || len(sign1.Unprotected.Other) != 0 {
		return errors.New("dari: unprotected map must be empty")
	}
	// Body/payload equality is part of conformance.
	if expectedPayload != nil && !bytes.Equal(sign1.Payload, expectedPayload) {
		return errors.New("dari: COSE payload does not equal the canonical body")
	}
	sigInput := []interface{}{"Signature1", sign1.Protected, externalAAD, sign1.Payload}
	sigBytes, err := MarshalCBOR(sigInput)
	if err != nil {
		return fmt.Errorf("dari: marshal sig structure for verify: %w", err)
	}
	if !ed25519.Verify(pub, sigBytes, sign1.Signature) {
		return errors.New("dari: COSE-Sign1 signature verification failed")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Authorization scope (F.4 CDDL).
// ---------------------------------------------------------------------------

// PathScope is one path-scope rule.
type PathScope struct {
	Authority  string   `cbor:"1,keyasint"`
	Revision   string   `cbor:"2,keyasint"`
	Prefix     string   `cbor:"3,keyasint"`
	Operations []string `cbor:"4,keyasint"`
}

// NetworkScope is one network-scope rule.
type NetworkScope struct {
	Scheme    string   `cbor:"1,keyasint"`
	Host      string   `cbor:"2,keyasint"`
	PortFirst uint16   `cbor:"3,keyasint"`
	PortLast  uint16   `cbor:"4,keyasint"`
	Purposes  []string `cbor:"5,keyasint"`
}

// AuthorizationScope is the grant's full permission set (label 11).
type AuthorizationScope struct {
	ActionClasses       []string          `cbor:"1,keyasint"`
	Models              []string          `cbor:"2,keyasint"`
	ReadPaths           []PathScope       `cbor:"3,keyasint"`
	WritePaths          []PathScope       `cbor:"4,keyasint"`
	Tools               []string          `cbor:"5,keyasint"`
	Networks            []NetworkScope    `cbor:"6,keyasint"`
	DataClassifications []string          `cbor:"7,keyasint"`
	ResourceBudgets     map[string]uint64 `cbor:"8,keyasint"`
	ProtectionProfiles  []string          `cbor:"9,keyasint"`
	ApprovalClasses     []string          `cbor:"10,keyasint"`
}

// AuthorizationGrantBody is the F.4 grant body (integer labels 1-16).
type AuthorizationGrantBody struct {
	Version              uint16             `cbor:"1,keyasint"`
	GrantID              string             `cbor:"2,keyasint"`
	Issuer               string             `cbor:"3,keyasint"`
	SubjectPeerID        string             `cbor:"4,keyasint"`
	SubjectKeyThumbprint Digest             `cbor:"5,keyasint"`
	Audience             []string           `cbor:"6,keyasint"`
	OrganizationID       string             `cbor:"7,keyasint"`
	UserID               string             `cbor:"8,keyasint"`
	SessionID            string             `cbor:"9,keyasint"`
	PolicyEpochID        string             `cbor:"10,keyasint"`
	Scope                AuthorizationScope `cbor:"11,keyasint"`
	NotBeforeMs          int64              `cbor:"12,keyasint"`
	NotAfterMs           int64              `cbor:"13,keyasint"`
	IssuerSequence       uint64             `cbor:"14,keyasint"`
	// ParentGrantDigest is nil on a root grant (omitted); a pointer so
	// omission is encodable — a fixed [32]byte is never "empty" under
	// the CBOR omitempty rules.
	ParentGrantDigest *Digest `cbor:"15,keyasint,omitempty"`
	// DelegationDepth is omitted when zero (F.4 rule 6).
	DelegationDepth uint8 `cbor:"16,keyasint,omitempty"`
}

// HasParentDigest reports whether the body carries a parent grant.
func (b *AuthorizationGrantBody) HasParentDigest() bool {
	return b.ParentGrantDigest != nil
}

// HasDelegationDepth reports whether the encoded body carries label 16.
func (b *AuthorizationGrantBody) HasDelegationDepth(canonical []byte) bool {
	return labelPresent(canonical, 16)
}

// labelPresent reports whether an integer label occurs as a top-level
// map key of a canonical CBOR encoding.
func labelPresent(canonical []byte, label uint64) bool {
	var m map[interface{}]interface{}
	if err := UnmarshalCBOR(canonical, &m); err != nil {
		return false
	}
	_, ok := m[label]
	return ok
}

// AuthorizationGrantAAD is the F.4 external AAD, terminal NUL included.
const AuthorizationGrantAAD = "DARI-AUTHORIZATION-GRANT-v1\x00"

// SubjectKeyThumbprint derives the F.3 subject-key thumbprint:
// SHA-256("DARI-SUBJECT-KEY-v1\0" || subject COSE_Key bytes). For the
// Ed25519 baseline the subject COSE_Key is the 32-byte public key.
func SubjectKeyThumbprint(pub ed25519.PublicKey) Digest {
	return KernelObjectDigestRaw("DARI-SUBJECT-KEY-v1\x00", COSEKeyCBOR(pub))
}

// ---------------------------------------------------------------------------
// Scope normalization and set validation (F.4: sets sorted by the
// deterministic encoding of each element, no duplicates).
// ---------------------------------------------------------------------------

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// NormalizeScope returns the scope with every set sorted and
// de-duplicated, and path prefixes normalized (no "." / ".." segments,
// "/" separators). Issuers MUST sign the normalized form; verifiers
// reject un-normalized forms. An invalid prefix is an error — the
// scope cannot be canonicalized.
func NormalizeScope(s AuthorizationScope) (AuthorizationScope, error) {
	var readErr, writeErr error
	s.ActionClasses = dedupSorted(sortedCopy(s.ActionClasses))
	s.Models = dedupSorted(sortedCopy(s.Models))
	s.Tools = dedupSorted(sortedCopy(s.Tools))
	s.DataClassifications = dedupSorted(sortedCopy(s.DataClassifications))
	s.ProtectionProfiles = dedupSorted(sortedCopy(s.ProtectionProfiles))
	s.ApprovalClasses = dedupSorted(sortedCopy(s.ApprovalClasses))
	s.ReadPaths, readErr = normalizePathScopes(s.ReadPaths)
	s.WritePaths, writeErr = normalizePathScopes(s.WritePaths)
	networks, err := normalizeNetworkScopes(s.Networks)
	if err != nil {
		return s, err
	}
	s.Networks = networks
	if readErr != nil {
		return s, readErr
	}
	return s, writeErr
}

func dedupSorted(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func normalizePathScopes(ps []PathScope) ([]PathScope, error) {
	out := make([]PathScope, 0, len(ps))
	for _, p := range ps {
		prefix, err := NormalizePathPrefix(p.Prefix)
		if err != nil {
			return nil, err
		}
		p.Operations = dedupSorted(sortedCopy(p.Operations))
		p.Prefix = prefix
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := MarshalCBOR(out[i])
		b, _ := MarshalCBOR(out[j])
		return bytes.Compare(a, b) < 0
	})
	// Sets: no duplicate rules (dedupe on encoded bytes).
	deduped := out[:0]
	for i, p := range out {
		if i == 0 || !bytes.Equal(mustEncode(out[i-1]), mustEncode(p)) {
			deduped = append(deduped, p)
		}
	}
	return deduped, nil
}

func mustEncode(v interface{}) []byte {
	b, _ := MarshalCBOR(v)
	return b
}

func normalizeNetworkScopes(ns []NetworkScope) ([]NetworkScope, error) {
	out := append([]NetworkScope(nil), ns...)
	for i := range out {
		if out[i].Scheme == "" || out[i].Host == "" {
			return nil, errors.New("dari: network scope requires scheme and host")
		}
		if out[i].PortFirst > out[i].PortLast {
			return nil, errors.New("dari: network scope port range inverted")
		}
		if len(out[i].Purposes) == 0 {
			return nil, errors.New("dari: network scope requires at least one purpose")
		}
		out[i].Purposes = dedupSorted(sortedCopy(out[i].Purposes))
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := MarshalCBOR(out[i])
		b, _ := MarshalCBOR(out[j])
		return bytes.Compare(a, b) < 0
	})
	deduped := out[:0]
	for i, p := range out {
		if i == 0 || !bytes.Equal(mustEncode(out[i-1]), mustEncode(p)) {
			deduped = append(deduped, p)
		}
	}
	return deduped, nil
}

// NormalizePathPrefix validates and normalizes a slash-delimited
// prefix: no "." or ".." segments, "/" separator, no duplicate
// slashes. An empty prefix is invalid (an empty permission set grants
// nothing; there is no root wildcard).
func NormalizePathPrefix(p string) (string, error) {
	if p == "" {
		return "", errors.New("dari: empty path prefix")
	}
	segs := []string{}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			return "", errors.New("dari: '..' segment in path prefix")
		}
		segs = append(segs, seg)
	}
	if len(segs) == 0 {
		return "", errors.New("dari: path prefix has no segments")
	}
	return strings.Join(segs, "/"), nil
}

// ---------------------------------------------------------------------------
// Grant encode / sign / verify.
// ---------------------------------------------------------------------------

// EncodeGrantBody returns the deterministic CBOR of the grant body
// with the scope normalized.
func EncodeGrantBody(b *AuthorizationGrantBody) ([]byte, error) {
	if b == nil {
		return nil, errors.New("dari: nil grant body")
	}
	scope, err := NormalizeScope(b.Scope)
	if err != nil {
		return nil, fmt.Errorf("dari: normalize grant scope: %w", err)
	}
	b.Scope = scope
	return MarshalCBOR(b)
}

// GrantEnvelope couples the signed COSE object with its decoded body
// and derived digests. The validator retains original signed bytes per
// F.4 step 2.
type GrantEnvelope struct {
	Body         *AuthorizationGrantBody
	COSE         *COSESign1
	COSEBytes    []byte // deterministic encoding of the COSE object
	BodyDigest   Digest // KernelObjectDigest(0x0202, body)
	SignedDigest Digest // KernelSignedObjectDigest(0x0202, cose)
	SignerKey    ed25519.PublicKey
}

// SignAuthorizationGrant signs the normalized body under the grant AAD
// and returns the envelope with digests derived.
func SignAuthorizationGrant(b *AuthorizationGrantBody, priv ed25519.PrivateKey) (*GrantEnvelope, error) {
	body, err := EncodeGrantBody(b)
	if err != nil {
		return nil, err
	}
	sign1, coseBytes, digest, err := SignKernelObject(b, AuthorizationGrantAAD, priv, ObjTypeAuthorizationGrant)
	if err != nil {
		return nil, err
	}
	return &GrantEnvelope{
		Body:         b,
		COSE:         sign1,
		COSEBytes:    coseBytes,
		BodyDigest:   KernelObjectDigest(ObjTypeAuthorizationGrant, body),
		SignedDigest: digest,
		SignerKey:    priv.Public().(ed25519.PublicKey),
	}, nil
}

// DecodeAuthorizationGrant verifies an envelope: canonical re-encode
// of the parsed body MUST equal the attached payload (F.2), the
// signature MUST verify under the signer key, and the protected
// header MUST be the exact dari/1 shape.
func DecodeAuthorizationGrant(coseBytes []byte, signer ed25519.PublicKey) (*GrantEnvelope, error) {
	body, sign1, digest, err := DecodeKernelObject(coseBytes, AuthorizationGrantAAD, EncodeGrantBody, signer)
	if err != nil {
		return nil, fmt.Errorf("dari: grant: %w", err)
	}
	reencoded, _ := EncodeGrantBody(body)
	return &GrantEnvelope{
		Body:         body,
		COSE:         sign1,
		COSEBytes:    coseBytes,
		BodyDigest:   KernelObjectDigest(ObjTypeAuthorizationGrant, reencoded),
		SignedDigest: digest,
		SignerKey:    signer,
	}, nil
}

// AuthorityResolver resolves the verification key for a grant's issuer
// (the policy issuer in the relay; a trust bundle in federation).
type AuthorityResolver interface {
	IssuerKey(issuer string) (ed25519.PublicKey, bool)
}

// VerifyGrantAuthority verifies a presented grant's signature and
// validity window under the resolved issuer key — the minimal live
// check brokers apply before any side effect.
func VerifyGrantAuthority(env *GrantEnvelope, resolver AuthorityResolver, nowMs int64) error {
	if env == nil || env.Body == nil || env.COSEBytes == nil {
		return errors.New("dari: nil grant envelope")
	}
	key, ok := resolver.IssuerKey(env.Body.Issuer)
	if !ok {
		return fmt.Errorf("dari: issuer %q not in authority set", env.Body.Issuer)
	}
	verified, err := DecodeAuthorizationGrant(env.COSEBytes, key)
	if err != nil {
		return fmt.Errorf("dari: grant signature: %w", err)
	}
	if nowMs < verified.Body.NotBeforeMs || nowMs >= verified.Body.NotAfterMs {
		return errors.New("dari: grant outside its validity window")
	}
	return nil
}

// COSEKeyCBOR renders the deterministic COSE_Key (RFC 9053 Ed25519:
// {1: OKP, -1: Ed25519, -2: key bytes}) — the thumbprint input per
// F.3 ("deterministic_cbor(subject COSE_Key)").
func COSEKeyCBOR(pub ed25519.PublicKey) []byte {
	body, err := MarshalCBOR(map[int]any{1: 1, -1: 6, -2: []byte(pub)})
	if err != nil {
		return nil // unreachable: deterministic encoding of plain ints/bytes
	}
	return body
}

// SignKernelObject is the shared canonical signer for every kernel
// object: canonical body → COSE with the object's AAD → envelope bytes
// + signed-object digest.
func SignKernelObject(body interface{}, aad string, priv ed25519.PrivateKey, objType ObjectType) (*COSESign1, []byte, Digest, error) {
	payload, err := MarshalCBOR(body)
	if err != nil {
		return nil, nil, Digest{}, err
	}
	kid := SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey))
	sign1, err := CreateCOSESign1WithAAD(payload, []byte(aad), priv, kid[:])
	if err != nil {
		return nil, nil, Digest{}, err
	}
	coseBytes, err := MarshalCBOR(sign1)
	if err != nil {
		return nil, nil, Digest{}, err
	}
	return sign1, coseBytes, KernelSignedObjectDigest(objType, coseBytes), nil
}

// DecodeKernelObject is the shared verifier: decode envelope → decode
// body → canonical re-encode must equal the attached payload →
// signature under the AAD → signed-object digest.
func DecodeKernelObject[T any](coseBytes []byte, aad string, canon func(*T) ([]byte, error), signer ed25519.PublicKey) (*T, *COSESign1, Digest, error) {
	var sign1 COSESign1
	if err := UnmarshalCBOR(coseBytes, &sign1); err != nil {
		return nil, nil, Digest{}, err
	}
	var body T
	if err := UnmarshalCBOR(sign1.Payload, &body); err != nil {
		return nil, nil, Digest{}, err
	}
	reencoded, err := canon(&body)
	if err != nil {
		return nil, nil, Digest{}, err
	}
	if !bytes.Equal(reencoded, sign1.Payload) {
		return nil, nil, Digest{}, errors.New("dari: body is not the canonical payload")
	}
	if err := VerifyCOSESign1WithAAD(&sign1, []byte(aad), reencoded, signer); err != nil {
		return nil, nil, Digest{}, err
	}
	return &body, &sign1, KernelSignedObjectDigest(objectTypeForAAD(aad), coseBytes), nil
}

// objectTypeForAAD maps a kernel AAD to its F.12 object type for
// signed-object digesting.
func objectTypeForAAD(aad string) ObjectType {
	switch aad {
	case AuthorizationGrantAAD:
		return ObjTypeAuthorizationGrant
	case DecisionAAD:
		return ObjTypeAuthorizationDecision
	case CheckpointAAD:
		return ObjTypeSignedStateCheckpoint
	case EffectPrepareAAD:
		return ObjTypeEffectPrepare
	case EffectAuthorizationAAD:
		return ObjTypeEffectAuthorize
	case EffectResultAAD:
		return ObjTypeEffectResult
	case EffectStatusAAD:
		return ObjTypeEffectStatusResp
	default:
		return 0xFFFF
	}
}

// PathPrefixCovers reports whether childPrefix is covered by
// parentPrefix per F.4: equal, or extends it with a '/' boundary.
func PathPrefixCovers(parentPrefix, childPrefix string) bool {
	if childPrefix == parentPrefix {
		return true
	}
	return len(childPrefix) > len(parentPrefix) &&
		childPrefix[:len(parentPrefix)] == parentPrefix &&
		childPrefix[len(parentPrefix)] == '/'
}
