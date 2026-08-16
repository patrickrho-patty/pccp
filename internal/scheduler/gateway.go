package scheduler

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler/queue"
)

// gateway.go implements the S2 unified gateway: OpenAI/Anthropic-compatible
// ingress, model rewriting, media controls, model discovery, correlation
// IDs, and cancellation (spec §14 rows 1, 12, 17). The gateway is the ONLY
// route to a worker — fail-closed by construction (spec §13.15).

// MediaControl configures the gateway's media limits (spec §14 row 12).
type MediaControl struct {
	MaxImagesPerRequest int
	MaxMediaBytes       int64
	AllowedSchemes      []string
	AllowedHosts        []string // empty = any public host
	BlockPrivateIPs     bool
}

// DefaultMediaControl returns safe defaults.
func DefaultMediaControl() MediaControl {
	return MediaControl{
		MaxImagesPerRequest: 8,
		MaxMediaBytes:       20 << 20,
		AllowedSchemes:      []string{"https"},
		BlockPrivateIPs:     true,
	}
}

// GatewayRequest is the normalized internal request after ingress parsing.
type GatewayRequest struct {
	ID              string
	CorrelationID   int64
	Tenant          string
	TrafficClass    string
	Model           string // external name (pre-rewrite)
	ResolvedModel   string // catalog ID (post-rewrite)
	InputTokens     int
	ExpectedOutput  int
	MaxOutputTokens int
	MediaTokens     int
}

// Gateway is the scheduler's request ingress. Safe for concurrent use.
type Gateway struct {
	dispatcher *Dispatcher
	rewriter   *ModelRewriter
	media      MediaControl
	queueTTL   time.Duration
	classes    *ClassResolver // nil = no issuer configured (fail-closed to batch)
}

// NewGateway builds the ingress over a dispatcher and optional rewriter.
func NewGateway(d *Dispatcher, rw *ModelRewriter) *Gateway {
	if rw == nil {
		rw = NewModelRewriter(nil)
	}
	return &Gateway{dispatcher: d, rewriter: rw, media: DefaultMediaControl(), queueTTL: time.Minute}
}

// Rewriter exposes the model rewriter (admin wiring).
func (g *Gateway) Rewriter() *ModelRewriter { return g.rewriter }

// SetTrafficIssuer installs the public key that signs traffic-class
// envelopes (the relay's service identity). Until set, every request is
// fail-closed to the lowest class — headers never elevate (spec §13.14).
func (g *Gateway) SetTrafficIssuer(pub ed25519.PublicKey) {
	g.classes = NewClassResolver(pub)
}

// SetQueueTTL bounds how long a request may park in the global queue
// before the gateway expires it with a retryable 503. Default 1 minute.
func (g *Gateway) SetQueueTTL(d time.Duration) { g.queueTTL = d }

// chatRequest is the OpenAI/Anthropic-common request shape.
type chatRequest struct {
	Model       string          `json:"model"`
	Messages    json.RawMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	MaxOutput   int             `json:"max_completion_tokens"`
	Temperature float64         `json:"temperature"`
	Stream      bool            `json:"stream"`
}

// HandleChatCompletions is the OpenAI-compatible ingress.
func (g *Gateway) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	g.handleIngress(w, r, false)
}

// HandleAnthropicMessages is the Anthropic-compatible ingress.
func (g *Gateway) HandleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	g.handleIngress(w, r, true)
}

// handleIngress validates, rewrites, measures, and enqueues one request.
// The response is 202 Accepted with the queue ID: late binding means the
// completion arrives through the dispatch channel, not synchronously here.
func (g *Gateway) handleIngress(w http.ResponseWriter, r *http.Request, anthropic bool) {
	if r.Method != http.MethodPost {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// Read the body once; media validation and token measurement share it.
	rawBody := mustReadBody(r)
	var req chatRequest
	if err := json.Unmarshal([]byte(rawBody), &req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == "" {
		writeGatewayError(w, http.StatusBadRequest, "model is required")
		return
	}
	maxOut := req.MaxTokens
	if req.MaxOutput > maxOut {
		maxOut = req.MaxOutput
	}
	if maxOut <= 0 {
		maxOut = 1024
	}

	corr := correlationID(r)
	resolved, err := g.rewriter.Resolve(req.Model, corr)
	if err != nil {
		writeGatewayError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Media controls before anything is queued (spec §14 row 12).
	if err := g.validateMedia(rawBody); err != nil {
		writeGatewayError(w, http.StatusBadRequest, err.Error())
		return
	}

	tenant, class, err := g.resolveTenantClass(r)
	if err != nil {
		writeGatewayError(w, http.StatusForbidden, err.Error())
		return
	}
	// Per-tenant model access: the resolved catalog ID must be in the
	// tenant's allow-list (spec §14 row 17; fail closed for tenants with
	// no configured access).
	if err := g.rewriter.CheckTenantAccess(tenant, resolved); err != nil {
		writeGatewayError(w, http.StatusForbidden, err.Error())
		return
	}

	inputTokens, mediaTokens := g.measureInput(req.Messages)
	expected := g.dispatcher.Estimator().Estimate(inputTokens, 0, maxOut)

	// Edge admission: shed overload before enqueue (spec §12.3.7).
	sig := g.dispatcher.FleetSignalsFromRegistry()
	if v := g.dispatcher.policy.EvaluateFor(sig, class); v == VerdictShed {
		writeGatewayError(w, http.StatusTooManyRequests, "fleet saturated; retry later")
		return
	}

	id := dari.GenerateID("gw-req")
	qr := queue.Request{
		ID:                   id,
		Tenant:               tenant,
		Class:                queue.Class(class),
		InputTokens:          inputTokens,
		ExpectedOutputTokens: expected,
		MaxOutputTokens:      maxOut,
		MediaTokens:          mediaTokens,
		ArrivedAt:            time.Now(),
		TTL:                  g.queueTTL,
		Payload:              RequestPayload{Model: resolved, Messages: req.Messages},
	}
	if req.Stream {
		ch, deltas, err := g.dispatcher.SubmitStream(qr)
		if err != nil {
			writeGatewayError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		g.serveStream(w, r, resolved, ch, deltas, id)
		return
	}

	ch, err := g.dispatcher.Submit(qr)
	if err != nil {
		writeGatewayError(w, http.StatusTooManyRequests, err.Error())
		return
	}

	// The caller's connection parks here until dispatch completes
	// (late binding: the request binds to a worker when capacity frees,
	// llm-d's blocked-handler design). TTL bounds the wait; the client's
	// disconnect cancels via the request context.
	select {
	case res := <-ch:
		g.writeCompletion(w, resolved, res)
	case <-time.After(g.queueTTL):
		g.dispatcher.Cancel(id)
		writeGatewayError(w, http.StatusServiceUnavailable, "queued request expired; retry")
	case <-r.Context().Done():
		g.dispatcher.Cancel(id)
		writeGatewayError(w, http.StatusServiceUnavailable, "request cancelled")
	}
}

// writeCompletion renders a terminal inference result to the caller.
func (g *Gateway) writeCompletion(w http.ResponseWriter, model string, res InferenceResult) {
	if res.Cancelled {
		writeGatewayError(w, http.StatusServiceUnavailable, "request cancelled")
		return
	}
	if res.Err != "" {
		writeGatewayError(w, http.StatusInternalServerError, res.Err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      dari.GenerateID("gw-cmp"),
		"object":  "chat.completion",
		"model":   model,
		"choices": []map[string]interface{}{{"index": 0, "message": map[string]string{"role": "assistant", "content": res.Text}, "finish_reason": res.Finish}},
		"usage":   res.Usage,
	})
}

// Cancel removes a queued request by ID (client disconnect propagation).
func (g *Gateway) Cancel(id string) bool { return g.dispatcher.Queue().Cancel(id) }

// HandleModelDiscovery lists models served by the fleet (spec §14 row 1:
// model discovery/readiness).
func (g *Gateway) HandleModelDiscovery(w http.ResponseWriter, r *http.Request) {
	models := g.dispatcher.ServedModels()
	sort.Strings(models)
	out := struct {
		Object string       `json:"object"`
		Data   []modelEntry `json:"data"`
	}{Object: "list", Data: make([]modelEntry, 0, len(models))}
	for _, m := range models {
		out.Data = append(out.Data, modelEntry{ID: m, Object: "model"})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

type modelEntry struct {
	ID     string `json:"id"`
	Object string `json:"object"`
}

// validateMedia enforces the media controls: scheme allowlist, host
// allowlist, private-IP blocking (SSRF), per-request count limit. The raw
// body was already buffered at ingress.
func (g *Gateway) validateMedia(rawBody string) error {
	// Media URLs live inside the message content; parse conservatively by
	// scanning the message content for image_url entries.
	var body struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal([]byte(rawBody), &body)

	count := 0
	for _, m := range body.Messages {
		var parts []struct {
			Type     string `json:"type"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if json.Unmarshal(m.Content, &parts) == nil {
			for _, p := range parts {
				if p.Type != "image_url" || p.ImageURL.URL == "" {
					continue
				}
				count++
				if count > g.media.MaxImagesPerRequest {
					return fmt.Errorf("media limit exceeded: max %d images per request", g.media.MaxImagesPerRequest)
				}
				if err := g.validateMediaURL(p.ImageURL.URL); err != nil {
					return err
				}
			}
			continue
		}
		var s string
		if json.Unmarshal(m.Content, &s) == nil {
			continue // plain text content
		}
	}
	return nil
}

// validateMediaURL applies the SSRF guardrails to one media URL.
func (g *Gateway) validateMediaURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid media url: %w", err)
	}
	okScheme := false
	for _, s := range g.media.AllowedSchemes {
		if u.Scheme == s {
			okScheme = true
			break
		}
	}
	if !okScheme {
		return fmt.Errorf("media scheme %q not allowed", u.Scheme)
	}
	if len(g.media.AllowedHosts) > 0 {
		okHost := false
		for _, h := range g.media.AllowedHosts {
			if strings.EqualFold(u.Hostname(), h) {
				okHost = true
				break
			}
		}
		if !okHost {
			return fmt.Errorf("media host %q not allowed", u.Hostname())
		}
	}
	if g.media.BlockPrivateIPs {
		ips, err := net.LookupIP(u.Hostname())
		if err != nil {
			return fmt.Errorf("media host resolution failed: %w", err)
		}
		for _, ip := range ips {
			if isPrivateIP(ip) {
				return fmt.Errorf("media url resolves to a private address (%s) — blocked", ip)
			}
		}
	}
	return nil
}

// isPrivateIP reports loopback, link-local, private, and metadata ranges.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() ||
		ip.Equal(net.IPv4(169, 254, 169, 254)) ||
		ip.Equal(net.IPv4zero)
}

// measureInput estimates input tokens and counts media parts from the raw
// messages JSON (tokenizer-exact accounting lives in the CP tokenizer
// service; the gateway estimate is the queue-debit signal, spec §12.3.3).
func (g *Gateway) measureInput(messages json.RawMessage) (tokens, media int) {
	// Conservative: ~4 bytes per token on UTF-8 Korean/mixed text.
	raw := string(messages)
	tokens = len(raw) / 4
	if tokens <= 0 {
		tokens = 1
	}
	media = strings.Count(raw, `"image_url"`)
	return tokens, media
}

// correlationID derives a deterministic int64 from the X-Correlation-ID
// header (or a fresh ID). Stable correlation ⇒ stable split decisions.
func correlationID(r *http.Request) int64 {
	if corr := r.Header.Get("X-Correlation-ID"); corr != "" {
		h := fnv.New64a()
		h.Write([]byte(corr))
		return int64(h.Sum64())
	}
	h := fnv.New64a()
	h.Write([]byte(r.RemoteAddr))
	h.Write([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	return int64(h.Sum64())
}

// mustReadBody buffers the request body for validation passes (bodies are
// bounded by the media byte limit upstream).
func mustReadBody(r *http.Request) string {
	body := r.Body
	defer body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return b.String()
}

// writeGatewayError emits a JSON error body.
func writeGatewayError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// fingerprint returns a short stable ID prefix for debugging.
func fingerprint(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// HandleEmbeddings serves the embeddings ingress (spec §14 row 1). The
// embedding request routes through the same queue/dispatch path as chat
// completions; the response is the raw forwarder result.
func (g *Gateway) HandleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rawBody := mustReadBody(r)
	var req struct {
		Model string `json:"model"`
		Input any    `json:"input"`
	}
	if err := json.Unmarshal([]byte(rawBody), &req); err != nil || req.Model == "" {
		writeGatewayError(w, http.StatusBadRequest, "model is required")
		return
	}
	corr := correlationID(r)
	resolved, err := g.rewriter.Resolve(req.Model, corr)
	if err != nil {
		writeGatewayError(w, http.StatusBadRequest, err.Error())
		return
	}
	tenant, class, terr := g.resolveTenantClass(r)
	if terr != nil {
		writeGatewayError(w, http.StatusForbidden, terr.Error())
		return
	}
	inputTokens := len(rawBody)/4 + 1
	expected := g.dispatcher.Estimator().Estimate(inputTokens, 0, 1024)

	id := dari.GenerateID("gw-emb")
	ch, err := g.dispatcher.Submit(queue.Request{
		ID:                   id,
		Tenant:               tenant,
		Class:                queue.Class(class),
		InputTokens:          inputTokens,
		ExpectedOutputTokens: expected,
		MaxOutputTokens:      1024,
		ArrivedAt:            time.Now(),
		TTL:                  g.queueTTL,
		Payload:              RequestPayload{Model: resolved, Messages: []byte(rawBody)},
	})
	if err != nil {
		writeGatewayError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	select {
	case res := <-ch:
		g.writeCompletion(w, resolved, res)
	case <-time.After(g.queueTTL):
		g.dispatcher.Cancel(id)
		writeGatewayError(w, http.StatusServiceUnavailable, "queued request expired; retry")
	case <-r.Context().Done():
		g.dispatcher.Cancel(id)
		writeGatewayError(w, http.StatusServiceUnavailable, "request cancelled")
	}
}

// serveStream emits SSE deltas as they arrive and terminates with the
// final completion chunk (OpenAI streaming contract, spec §14 row 1).
func (g *Gateway) serveStream(w http.ResponseWriter, r *http.Request, model string, ch <-chan InferenceResult, deltas <-chan string, id string) {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	writeSSE := func(data string) {
		if _, err := w.Write([]byte(data)); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}

	for {
		select {
		case delta, ok := <-deltas:
			if !ok {
				deltas = nil
				continue
			}
			payload, _ := json.Marshal(map[string]interface{}{
				"id": id, "object": "chat.completion.chunk", "model": model,
				"choices": []map[string]interface{}{{"index": 0, "delta": map[string]string{"content": delta}, "finish_reason": nil}},
			})
			writeSSE("data: " + string(payload) + "\n\n")
		case res, ok := <-ch:
			if !ok {
				return
			}
			// Drain any deltas still buffered before the terminal chunk:
			// the result and deltas channels are both ready at completion,
			// and ordering must be preserved (select would race them).
			if deltas != nil {
				for {
					select {
					case delta, more := <-deltas:
						if !more {
							deltas = nil
							break
						}
						payload, _ := json.Marshal(map[string]interface{}{
							"id": id, "object": "chat.completion.chunk", "model": model,
							"choices": []map[string]interface{}{{"index": 0, "delta": map[string]string{"content": delta}, "finish_reason": nil}},
						})
						writeSSE("data: " + string(payload) + "\n\n")
					default:
						deltas = nil
						break
					}
					if deltas == nil {
						break
					}
				}
			}
			finish := "error"
			if !res.Cancelled && res.Err == "" {
				finish = res.Finish
			}
			payload, _ := json.Marshal(map[string]interface{}{
				"id": id, "object": "chat.completion.chunk", "model": model,
				"choices": []map[string]interface{}{{"index": 0, "delta": map[string]string{}, "finish_reason": finish}},
				"usage":   res.Usage,
			})
			writeSSE("data: " + string(payload) + "\n\n")
			writeSSE("data: [DONE]\n\n")
			return
		case <-r.Context().Done():
			g.dispatcher.Cancel(id)
			return
		}
	}
}

// decodeTrafficEnvelope parses the X-Traffic-Envelope header (base64 JSON
// of a signed TrafficEnvelope). Returns nil when absent or malformed —
// the resolver's fail-closed default applies.
// resolveTenantClass derives the authoritative tenant and traffic class
// from the request (spec §13.14: tenant/priority metadata arrives in the
// COSE-signed envelope, never from client headers). When a valid envelope
// is present, its TenantID is authoritative; a conflicting X-Tenant-ID
// header is rejected as tampering rather than trusted. Without an
// envelope the request keeps the header/default tenant but is pinned to
// the batch class — the gateway's internal-caller path (the relay always
// signs).
func (g *Gateway) resolveTenantClass(r *http.Request) (tenant string, class string, err error) {
	tenant = r.Header.Get("X-Tenant-ID")
	if tenant == "" {
		tenant = "default"
	}
	class = string(queue.ClassBatch)
	env := decodeTrafficEnvelope(r)
	if g.classes == nil || env == nil || env.SignatureHex == "" {
		return tenant, class, nil
	}
	if vErr := env.Verify(g.classes.issuerPub); vErr != nil {
		// Invalid signature: fail closed to batch; the header tenant is
		// untrusted metadata but still subject to its (own) allow-list.
		return tenant, class, nil
	}
	// A validly-signed envelope carries a non-empty tenant (the relay
	// always binds one). An empty tenant on a valid envelope is
	// malformed — refuse rather than silently trusting the header.
	if env.TenantID == "" {
		return "", "", fmt.Errorf("scheduler: signed envelope carries no tenant")
	}
	if env.TenantID != tenant {
		return "", "", fmt.Errorf("scheduler: tenant header %q conflicts with signed envelope tenant %q", tenant, env.TenantID)
	}
	return env.TenantID, env.Class, nil
}

func decodeTrafficEnvelope(r *http.Request) *TrafficEnvelope {
	raw := r.Header.Get("X-Traffic-Envelope")
	if raw == "" {
		return nil
	}
	var env TrafficEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil
	}
	return &env
}
