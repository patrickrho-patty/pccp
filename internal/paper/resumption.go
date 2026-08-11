package paper

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// SessionResumptionRequest requests resumption of a disconnected session (PAPER §53).
type SessionResumptionRequest struct {
	WorkingSessionID    string `cbor:"1,keyasint"`
	ResumptionToken     []byte `cbor:"2,keyasint"`
	LastAckLaneSeq      uint64 `cbor:"3,keyasint"`
	LastEvidenceReceipt []byte `cbor:"4,keyasint,omitempty"`
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
	SessionID     string
	Token         []byte
	IssuedAt      time.Time
	ExpiresAt     time.Time
	LastSeqNum    uint64
	OrgID         string
	UserID        string
	HarnessID     string
}

// GenerateResumptionToken creates a resumption token for a session.
func GenerateResumptionToken(sessionID, orgID string) *ResumptionCredential {
	now := time.Now()
	h := sha256.New()
	h.Write([]byte(sessionID))
	h.Write([]byte(orgID))
	h.Write([]byte(fmt.Sprintf("%d", now.UnixNano())))
	token := h.Sum(nil)

	return &ResumptionCredential{
		SessionID: sessionID,
		Token:     token,
		IssuedAt:  now,
		ExpiresAt: now.Add(30 * time.Minute), // 30-minute resumption window
		OrgID:     orgID,
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

// LaneResumability declares whether a lane can be resumed (PAPER §53).
type LaneResumability int

const (
	LaneNonResumable       LaneResumability = 0
	LaneResumeFromAck      LaneResumability = 1
	LaneResumeFromStart    LaneResumability = 2
)

// IdempotencyClassForInferenceDisconnect handles inference disconnect semantics (PAPER §54).
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
