package dari

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// SessionResumptionRequest requests resumption of a disconnected session (DARI §53).
type SessionResumptionRequest struct {
	WorkingSessionID    string   `cbor:"1,keyasint"`
	ResumptionToken     []byte   `cbor:"2,keyasint"`
	LastAckLaneSeq      uint64   `cbor:"3,keyasint"`
	LastEvidenceReceipt []byte   `cbor:"4,keyasint,omitempty"`
	ActiveExchangeIDs   []string `cbor:"5,keyasint,omitempty"`
}

// SessionResumptionResponse responds to a resumption request.
type SessionResumptionResponse struct {
	Granted             bool     `cbor:"1,keyasint"`
	Reason              string   `cbor:"2,keyasint,omitempty"`
	ResumedLaneIDs      []uint64 `cbor:"3,keyasint,omitempty"`
	ResumedFromSeq      uint64   `cbor:"4,keyasint,omitempty"`
	NewLeaseID          string   `cbor:"5,keyasint,omitempty"`
	RequiresFullRestart bool     `cbor:"6,keyasint,omitempty"`
}

// ResumptionCredential is a token that allows session resumption.
type ResumptionCredential struct {
	SessionID  string    `json:"session_id"`
	Token      []byte    `json:"token"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastSeqNum uint64    `json:"last_seq_num"`
	OrgID      string    `json:"org_id"`
	UserID     string    `json:"user_id"`
	HarnessID  string    `json:"harness_id"`
}

// GenerateResumptionToken creates a resumption credential for a
// session. The token is 32 bytes of CRYPTO-RAND — a resumption token
// re-binds session governance to whichever connection presents it, so
// a derivable token (hash of guessable inputs) is a session-hijack
// vector. HarnessID binds the credential to the enrolled peer.
func GenerateResumptionToken(sessionID, orgID, harnessID string) *ResumptionCredential {
	now := time.Now()
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		panic("dari: resumption token entropy: " + err.Error())
	}
	return &ResumptionCredential{
		SessionID: sessionID,
		Token:     token,
		IssuedAt:  now,
		ExpiresAt: now.Add(30 * time.Minute), // 30-minute resumption window
		OrgID:     orgID,
		HarnessID: harnessID,
	}
}

// IsValid checks if a resumption credential is still valid.
func (rc *ResumptionCredential) IsValid() bool {
	return time.Now().Before(rc.ExpiresAt)
}

// TokenHex returns the hex-encoded token.
func (rc *ResumptionCredential) TokenHex() string {
	return hex.EncodeToString(rc.Token)
}

// LaneResumability declares whether a lane can be resumed (DARI §53).
type LaneResumability int

const (
	LaneNonResumable    LaneResumability = 0
	LaneResumeFromAck   LaneResumability = 1
	LaneResumeFromStart LaneResumability = 2
)

// IdempotencyClassForInferenceDisconnect handles inference disconnect semantics (DARI §54).
type InferenceDisconnectAction string

const (
	DisconnectRetry      InferenceDisconnectAction = "retry"
	DisconnectQueryState InferenceDisconnectAction = "query_state"
	DisconnectFail       InferenceDisconnectAction = "fail"
)

// ShouldRetryOnDisconnect determines what to do when an inference connection drops.
func ShouldRetryOnDisconnect(hadSideEffect bool, hasIdempotencyKey bool) InferenceDisconnectAction {
	if hadSideEffect {
		if hasIdempotencyKey {
			return DisconnectQueryState
		}
		return DisconnectFail
	}
	return DisconnectRetry
}
