package pia

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// EngineInfo is the best-effort introspection snapshot of the local serving
// engine. Every field is optional; failures degrade the card, not the PIA.
type EngineInfo struct {
	ModelName         string
	ModelVersion      string
	Precision         string
	ContextLength     uint64
	MaxConcurrentSeqs uint64
	Modalities        []string
	EngineVersion     string
}

// GPUInfo is the host GPU inventory snapshot.
type GPUInfo struct {
	AcceleratorFamily string
	SKU               string
	Count             uint32
	HBMGB             uint32
}

// introspectEngine queries the engine's OpenAI-compatible /v1/models and
// Prometheus /metrics endpoints (root base URL, matching VLLMAdapter). The
// engine is local; timeouts are short.
func introspectEngine(baseURL string) (EngineInfo, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	base := strings.TrimSuffix(baseURL, "/")

	info := EngineInfo{}
	resp, err := client.Get(base + "/v1/models")
	if err != nil {
		return info, fmt.Errorf("pia: engine /models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("pia: engine /models status %d", resp.StatusCode)
	}
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return info, fmt.Errorf("pia: decode /models: %w", err)
	}
	if len(modelsResp.Data) == 0 {
		return info, fmt.Errorf("pia: engine reported no models")
	}
	info.ModelName = modelsResp.Data[0].ID

	// Best-effort metrics pass: absence is not an error.
	metricsResp, err := client.Get(base + "/metrics")
	if err == nil {
		defer metricsResp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(metricsResp.Body, 1<<20))
		info.MaxConcurrentSeqs = parseMetricUint(string(body), "vllm:num_requests_running")
		if info.MaxConcurrentSeqs == 0 {
			info.MaxConcurrentSeqs = parseMetricUint(string(body), "sglang:running_requests")
		}
	}
	return info, nil
}

// parseMetricUint extracts a numeric metric value from Prometheus text.
func parseMetricUint(text, name string) uint64 {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, name+" ") || strings.HasPrefix(line, name+"{") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if v, err := strconv.ParseUint(fields[len(fields)-1], 10, 64); err == nil {
					return v
				}
			}
		}
	}
	return 0
}

// detectGPUs inventories the host via nvidia-smi (no native NVML dependency
// in S1). Absence of the tool degrades to an unknown inventory, not a
// failure — dev machines and non-NVIDIA hosts are legitimate.
func detectGPUs() GPUInfo {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=name,memory.total,count",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return GPUInfo{AcceleratorFamily: "unknown", SKU: "unknown"}
	}
	line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	fields := strings.Split(line, ",")
	if len(fields) < 3 {
		return GPUInfo{AcceleratorFamily: "unknown", SKU: "unknown"}
	}
	info := GPUInfo{AcceleratorFamily: "nvidia", SKU: strings.TrimSpace(fields[0])}
	if total, err := strconv.ParseUint(strings.TrimSpace(fields[1]), 10, 64); err == nil {
		info.HBMGB = uint32(total / 1024)
	}
	if count, err := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64); err == nil {
		info.Count = uint32(count)
	}
	if info.Count == 0 {
		info.Count = 1
	}
	return info
}

// MeasureReachability reports the engine's measured backend grade by
// inspecting the actual listening socket: the config can claim localhost
// while the engine binds a wildcard address, so the card reports reality
// (DARI scheduler §8).
func MeasureReachability(engineURL string) string {
	u, err := url.Parse(engineURL)
	if err != nil {
		return "unknown"
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return "localhost"
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return "unknown"
	}

	// Linux: read the kernel socket table directly.
	if table, err := os.ReadFile("/proc/net/tcp"); err == nil {
		if addr, ok := findListenEntry(string(table), portNum); ok {
			return gradeFromBindAddress(addr)
		}
	}
	if table, err := os.ReadFile("/proc/net/tcp6"); err == nil {
		if addr, ok := findListenEntry(string(table), portNum); ok {
			return gradeFromBindAddress(addr)
		}
	}

	// Portable fallback: lsof.
	out, err := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", portNum), "-sTCP:LISTEN").Output()
	if err == nil {
		addr := parseLsofListen(string(out), portNum)
		if addr != "" {
			return gradeFromBindAddress(addr)
		}
	}
	return "unknown"
}

// findListenEntry returns the local bind address of the LISTEN socket for
// the given port in a /proc/net/tcp{,6} table, if present.
func findListenEntry(table string, port int) (string, bool) {
	want := fmt.Sprintf(":%04X", port)
	for _, line := range strings.Split(table, "\n") {
		addr, linePort, state, ok := parseTCPTableLine(line)
		if !ok {
			continue
		}
		if linePort == port && state == "0A" {
			if strings.HasSuffix(addr, want) || addr != "" {
				return addr, true
			}
		}
	}
	return "", false
}

// parseTCPTableLine parses one /proc/net/tcp{,6} entry into (local address,
// local port, connection state hex, ok). Addresses are decoded to textual
// IPv4 or "::" for the IPv6 wildcard.
func parseTCPTableLine(line string) (string, int, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return "", 0, "", false
	}
	local := fields[1]
	addrPort := strings.Split(local, ":")
	if len(addrPort) != 2 {
		return "", 0, "", false
	}
	port, err := strconv.ParseUint(addrPort[1], 16, 32)
	if err != nil {
		return "", 0, "", false
	}
	addr := hexAddrToText(addrPort[0])
	return addr, int(port), fields[3], true
}

// hexAddrToText converts a /proc/net/tcp{,6} hex address to textual form.
func hexAddrToText(hexAddr string) string {
	if len(hexAddr) == 8 {
		// IPv4, little-endian u32.
		v, err := strconv.ParseUint(hexAddr, 16, 32)
		if err != nil {
			return ""
		}
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(v))
		return net.IP(b).String()
	}
	if hexAddr == strings.Repeat("0", 32) {
		return "::"
	}
	if hexAddr == "00000000000000000000000001000000" {
		return "::1"
	}
	return ""
}

// gradeFromBindAddress maps a measured bind address to a reachability grade.
func gradeFromBindAddress(addr string) string {
	switch addr {
	case "127.0.0.1", "::1":
		return "localhost"
	case "0.0.0.0", "::":
		return "exposed"
	case "":
		return "unknown"
	default:
		return "private"
	}
}

// parseLsofListen extracts the local bind address from lsof LISTEN output
// for the given port.
func parseLsofListen(out string, port int) string {
	want := fmt.Sprintf(":%d", port)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[len(fields)-1]
		if !strings.HasSuffix(name, want) {
			continue
		}
		addr := strings.TrimSuffix(name, want)
		switch addr {
		case "*", "0.0.0.0", "[::]":
			return "0.0.0.0"
		case "127.0.0.1", "localhost", "[::1]":
			return "127.0.0.1"
		default:
			return addr
		}
	}
	return ""
}
