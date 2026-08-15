package pia

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseTCPTableLineLocalhost(t *testing.T) {
	line := "   0: 0100007F:1F5F 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 10046 1 0000000000000000 100 0 0 10 0"
	addr, port, state, ok := parseTCPTableLine(line)
	if !ok {
		t.Fatal("line not parsed")
	}
	if addr != "127.0.0.1" {
		t.Fatalf("addr %q, want 127.0.0.1", addr)
	}
	if port != 8031 {
		t.Fatalf("port %d, want 8031", port)
	}
	if state != "0A" {
		t.Fatalf("state %q, want 0A (LISTEN)", state)
	}
}

func TestParseTCPTableLineWildcard(t *testing.T) {
	line := "   0: 00000000:1F60 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 10047 1 0000000000000000 100 0 0 10 0"
	addr, port, _, ok := parseTCPTableLine(line)
	if !ok {
		t.Fatal("line not parsed")
	}
	if addr != "0.0.0.0" {
		t.Fatalf("addr %q, want 0.0.0.0", addr)
	}
	if port != 8032 {
		t.Fatalf("port %d, want 8032", port)
	}
}

func TestGradeFromBindAddress(t *testing.T) {
	cases := []struct {
		addr string
		want string
	}{
		{"127.0.0.1", "localhost"},
		{"::1", "localhost"},
		{"0.0.0.0", "exposed"},
		{"::", "exposed"},
		{"10.200.82.233", "private"},
		{"", "unknown"},
	}
	for _, c := range cases {
		if got := gradeFromBindAddress(c.addr); got != c.want {
			t.Fatalf("gradeFromBindAddress(%q) = %q, want %q", c.addr, got, c.want)
		}
	}
}

func TestFindListenEntry(t *testing.T) {
	table := `
   0: 0100007F:1F5F 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 10046 1 0000000000000000 100 0 0 10 0
   0: 00000000:1F60 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 10047 1 0000000000000000 100 0 0 10 0
`
	if addr, ok := findListenEntry(table, 8031); !ok || addr != "127.0.0.1" {
		t.Fatalf("findListenEntry(8031) = %q/%v", addr, ok)
	}
	if _, ok := findListenEntry(table, 9999); ok {
		t.Fatal("unexpected entry for port 9999")
	}
}

func TestIntrospectEngineModels(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"object":"list","data":[{"id":"Qwen3.6-27B-FP8","object":"model","owned_by":"pccp"}]}`))
			return
		}
		if r.URL.Path == "/metrics" {
			w.Write([]byte("vllm:num_requests_running 7\nvllm:gpu_cache_usage_perc 0.42\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer engine.Close()

	info, err := introspectEngine(engine.URL)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if info.ModelName != "Qwen3.6-27B-FP8" {
		t.Fatalf("model %q", info.ModelName)
	}
	if info.MaxConcurrentSeqs != 7 {
		t.Fatalf("seqs %d, want 7", info.MaxConcurrentSeqs)
	}
}

func TestIntrospectEngineUnreachable(t *testing.T) {
	engine := httptest.NewServer(http.NotFoundHandler())
	addr := engine.URL
	engine.Close()

	if _, err := introspectEngine(addr); err == nil {
		t.Fatal("expected error for unreachable engine")
	}
}
