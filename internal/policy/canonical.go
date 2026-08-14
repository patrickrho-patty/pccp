package policy

import (
	"encoding/binary"
	"sort"
)

// This file mirrors — byte-for-byte — the connector's canonical lease
// signing body (`patty-code-pccp/internal/dariproto/lease.go::
// Lease.SigningBytes`). The relay signs this body; the connector's
// LeaseVerifier recomputes it and rejects any drift. The
// `internal/lease_conformance` suite pins the layout; the relay-side
// fixture there must equal this implementation.

// LeaseDomain is the domain-separation prefix. It must match the
// connector's `dariproto.LeaseDomain` exactly.
const LeaseDomain = "DARI-CAPABILITY-LEASE-v1\x00"

// LeaseSigningInput is the exact field set bound by the canonical
// lease signature. Every field the relay issues onto the wire must be
// a member — an unsigned field is a forgeable field.
type LeaseSigningInput struct {
	LeaseID            string
	SubjectPeerID      string
	UserID             string
	SessionID          string
	PolicyEpochID      string
	AllowedModels      []string
	FilePathReadScope  []string
	FilePathWriteScope []string
	ToolClasses        []string
	RepositoryScope    []map[string]string
	TokenBudget        int64
	NotBeforeUnixMs    int64
	NotAfterUnixMs     int64
	LeaseSequence      uint64
	IssuedAtUnixMs     int64
}

// CanonicalLeaseSigningBytes renders the canonical byte string. Field
// order, length prefixes, and endianness are pinned by the cross-repo
// conformance suite; do not change without updating both repos.
func CanonicalLeaseSigningBytes(in LeaseSigningInput) []byte {
	dst := make([]byte, 0, 256)
	dst = append(dst, LeaseDomain...)
	dst = lpString(dst, in.LeaseID)
	dst = lpString(dst, in.SubjectPeerID)
	dst = lpString(dst, in.UserID)
	dst = lpString(dst, in.SessionID)
	dst = lpString(dst, in.PolicyEpochID)
	dst = lpStringSlice(dst, in.AllowedModels)
	dst = lpStringSlice(dst, in.FilePathReadScope)
	dst = lpStringSlice(dst, in.FilePathWriteScope)
	dst = lpStringSlice(dst, in.ToolClasses)
	dst = lpRepoScope(dst, in.RepositoryScope)
	dst = lpU64(dst, uint64(in.TokenBudget))
	dst = lpU64(dst, uint64(in.NotBeforeUnixMs))
	dst = lpU64(dst, uint64(in.NotAfterUnixMs))
	dst = lpU64(dst, in.LeaseSequence)
	dst = lpU64(dst, uint64(in.IssuedAtUnixMs))
	return dst
}

func lpString(dst []byte, v string) []byte {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(v)))
	dst = append(dst, lenBuf[:]...)
	return append(dst, v...)
}

func lpStringSlice(dst []byte, values []string) []byte {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(values)))
	dst = append(dst, lenBuf[:]...)
	for _, v := range values {
		dst = lpString(dst, v)
	}
	return dst
}

func lpRepoScope(dst []byte, scopes []map[string]string) []byte {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(scopes)))
	dst = append(dst, lenBuf[:]...)
	for _, scope := range scopes {
		keys := make([]string, 0, len(scope))
		for k := range scope {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(keys)))
		dst = append(dst, lenBuf[:]...)
		for _, k := range keys {
			dst = lpString(dst, k)
			dst = lpString(dst, scope[k])
		}
	}
	return dst
}

func lpU64(dst []byte, value uint64) []byte {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], 8)
	dst = append(dst, lenBuf[:]...)
	var valBuf [8]byte
	binary.BigEndian.PutUint64(valBuf[:], value)
	return append(dst, valBuf[:]...)
}
