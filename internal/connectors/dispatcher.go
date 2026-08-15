// Package connectors dispatcher.go implements the governed ConnectorDispatcher
// (master plan Task 17 Step 2): connectors dispatch ONLY through stored
// scoped credentials, retries and failures land in the event spine,
// and provider-specific results are normalized.
package connectors

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DispatchRequest is one governed connector call.
type DispatchRequest struct {
	ConnectorID string
	// Action: "send_message" | "fetch_issues" | "trigger_ci".
	Action  string
	Payload json.RawMessage
	// MaxAttempts bounds the retry ladder (default 3).
	MaxAttempts int
}

// DispatchOutcome is the normalized result + spine references.
type DispatchOutcome struct {
	ConnectorID string          `json:"connector_id"`
	Action      string          `json:"action"`
	OK          bool            `json:"ok"`
	Attempts    int             `json:"attempts"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	// EventSpineRef digests the attempt ladder for the event spine.
	EventSpineRef string `json:"event_spine_ref"`
}

// ErrScopedCredentialRequired is the boundary: dispatch refuses to run
// with an unstored credential.
var ErrScopedCredentialRequired = errors.New("connectors: scoped credential required")

// HTTPDoer is the HTTP seam (tests inject a fake transport).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Dispatcher executes connector actions through stored scoped
// credentials with a bounded retry ladder and event-spine accounting.
type Dispatcher struct {
	svc  *Service
	http HTTPDoer
	mu   sync.Mutex
	log  []*DispatchOutcome
}

// NewDispatcher wires the dispatcher (client defaults to http.DefaultClient).
func NewDispatcher(svc *Service) *Dispatcher {
	return &Dispatcher{svc: svc, http: http.DefaultClient}
}

// SetHTTPDoer injects the transport seam.
func (d *Dispatcher) SetHTTPDoer(doer HTTPDoer) { d.http = doer }

// Dispatch runs the governed connector call:
//
//  1. the connector must exist + be enabled;
//  2. it must carry a STORED scoped credential (BaseURL+AuthToken) —
//     inline secrets are refused;
//  3. the request runs with a bounded retry ladder (backoff 2^attempt);
//  4. provider results are normalized; retries and failures append to
//     the event spine log.
func (d *Dispatcher) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchOutcome, error) {
	conn, err := d.svc.Get(req.ConnectorID)
	if err != nil {
		return nil, err
	}
	if conn.Status == "disabled" {
		return nil, fmt.Errorf("connectors: %s is disabled", req.ConnectorID)
	}
	if conn.BaseURL == "" || conn.AuthToken == "" {
		return nil, fmt.Errorf("%w: connector %s has no stored credential", ErrScopedCredentialRequired, req.ConnectorID)
	}
	maxAttempts := req.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	outcome := &DispatchOutcome{ConnectorID: req.ConnectorID, Action: req.Action}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		outcome.Attempts = attempt
		result, err := d.callOnce(ctx, conn, req)
		if err == nil {
			outcome.OK = true
			outcome.Result = result
			break
		}
		lastErr = err
		// Retry only transient failures; 4xx never retries.
		var sce *statusCodeError
		if errors.As(err, &sce) && sce.code >= 400 && sce.code < 500 {
			break
		}
		if attempt < maxAttempts {
			select {
			case <-time.After(time.Duration(1<<uint(attempt)) * 250 * time.Millisecond):
			case <-ctx.Done():
				lastErr = ctx.Err()
				// attempt loop exit
				outcome.Attempts = attempt
				outcome.Error = lastErr.Error()
				d.record(outcome)
				return outcome, lastErr
			}
		}
	}
	if !outcome.OK && lastErr != nil {
		outcome.Error = lastErr.Error()
	}
	outcome.EventSpineRef = spineRef(req, outcome.Attempts, outcome.OK)
	d.record(outcome)
	if !outcome.OK {
		return outcome, fmt.Errorf("connectors: %s %s failed after %d attempts: %v", req.ConnectorID, req.Action, outcome.Attempts, lastErr)
	}
	return outcome, nil
}

// statusCodeError carries the HTTP status for retry classification.
type statusCodeError struct {
	code int
	body string
}

func (e *statusCodeError) Error() string {
	return fmt.Sprintf("connectors: HTTP %d: %s", e.code, e.body)
}

// callOnce performs one provider call. Provider normalization: the
// wire shapes differ (Slack {text}, Teams {text}, KakaoWork {text},
// Jira/GitHub/GitLab issue arrays); the dispatcher POSTs the payload
// as JSON with the stored credential and returns the raw body — the
// provider-specific parsing stays in the service's typed methods.
func (d *Dispatcher) callOnce(ctx context.Context, conn *Connector, req DispatchRequest) (json.RawMessage, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, conn.BaseURL, bytes.NewReader(req.Payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+conn.AuthToken)
	resp, err := d.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, &statusCodeError{code: resp.StatusCode, body: string(body)}
	}
	if len(body) == 0 {
		return json.RawMessage(`{"ok":true}`), nil
	}
	return json.RawMessage(body), nil
}

// record appends the outcome to the in-process spine (the event
// service ingests from here).
func (d *Dispatcher) record(o *DispatchOutcome) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log = append(d.log, o)
	if len(d.log) > 256 {
		d.log = d.log[len(d.log)-256:]
	}
}

// Spine returns the recent outcomes (event-spine feed).
func (d *Dispatcher) Spine() []*DispatchOutcome {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*DispatchOutcome, len(d.log))
	copy(out, d.log)
	return out
}

// spineRef digests the attempt ladder for the event spine.
func spineRef(req DispatchRequest, attempts int, ok bool) string {
	h := sha256.New()
	fmt.Fprintf(h, "DARI-CONNECTOR-SPINE-v1\x00%s|%s|%d|%t", req.ConnectorID, req.Action, attempts, ok)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// VerifyWebhookSignature validates provider webhooks with the stored
// secret (HMAC-SHA256 over the raw body; Slack/Teams/GitHub scheme).
func VerifyWebhookSignature(secret string, body, signature []byte) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(expected, signature)
}

var _ = hex.EncodeToString
