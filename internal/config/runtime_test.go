package config

import (
	"os"
	"path/filepath"
	"testing"
)

// runtime_test.go pins the TOML surface: file → getters, env override,
// reload swap, malformed-file safety.
func TestRuntimeFileAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pccp.toml")
	if err := os.WriteFile(path, []byte(`
relay_admin_url = "http://10.0.0.5:8091"
relay_probe_addr = "10.0.0.5:8090"
min_harness_version = "1.4.0"
session_ttl_seconds = 7200
web_origin_allowlist = ["https://ui.example"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PCCP_CONFIG", path)
	t.Setenv("PCCP_PIA_PROBE_ADDR", "10.9.9.9:9090") // env overrides file-absent key
	if err := Reload(); err != nil {
		t.Fatal(err)
	}
	if got := RelayAdminURL(); got != "http://10.0.0.5:8091" {
		t.Fatalf("relay_admin_url = %q", got)
	}
	if got := MinHarnessVersion(); got != "1.4.0" {
		t.Fatalf("min_harness_version = %q", got)
	}
	if got := PIAProbeAddr(); got != "10.9.9.9:9090" {
		t.Fatalf("env override = %q", got)
	}
	if s, i := SessionTTLs(); s != 7200 || i != 1800 {
		t.Fatalf("ttls = %d/%d", s, i)
	}
	if got := WebOriginAllowlist(); len(got) != 1 || got[0] != "https://ui.example" {
		t.Fatalf("origins = %v", got)
	}

	// Edit + reload: new value lands atomically.
	if err := os.WriteFile(path, []byte(`relay_admin_url = "http://10.0.0.6:8091"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Reload(); err != nil {
		t.Fatal(err)
	}
	if got := RelayAdminURL(); got != "http://10.0.0.6:8091" {
		t.Fatalf("after reload = %q", got)
	}

	// Malformed file: reload errors and the PREVIOUS snapshot survives.
	if err := os.WriteFile(path, []byte("relay_admin_url = [not toml"), 0x600); err != nil {
		t.Fatal(err)
	}
	if err := Reload(); err == nil {
		t.Fatal("malformed file must error")
	}
	if got := RelayAdminURL(); got != "http://10.0.0.6:8091" {
		t.Fatalf("malformed reload must keep previous value, got %q", got)
	}
}
