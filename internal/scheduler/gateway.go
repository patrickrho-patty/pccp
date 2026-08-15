package scheduler

import (
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
}

// NewGateway builds the ingress over a dispatcher and optional rewriter.
func NewGateway(d *Dispatcher, rw *ModelRewriter) *Gateway {
	if rw == nil {
		rw = NewModelRewriter(nil)
	}
	return &Gateway{dispatcher: d, rewriter: rw, media: DefaultMediaControl()}
}

// Rewriter exposes the model rewriter (admin wiring).
func (g *Gateway) Rewriter() *ModelRewriter { return g.rewriter }

// chatRequest is the OpenAI/Anthropic-common request shape.
type chatRequest struct {
	Model       string          `json:"model"`
	Messages    json.RawMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	MaxOutput   int             `json:"max_completion_tokens"`
	Temperature float64         `json:"temperature"`
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

	tenant := r.Header.Get("X-Tenant-ID")
	if tenant == "" {
		tenant = "default"
	}
	class := r.Header.Get("X-Traffic-Class")
	if class == "" {
		class = string(queue.ClassInteractiveNormal)
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
		TTL:                  time.Minute,
		Payload:              resolved,
	}
	if err := g.dispatcher.Queue().Enqueue(qr); err != nil {
		writeGatewayError(w, http.StatusTooManyRequests, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     id,
		"status": "queued",
		"model":  resolved,
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
