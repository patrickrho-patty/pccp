// Package config — runtime.go: the RELOADABLE runtime settings.
//
// Cross-process addresses and policy floors live in a TOML file
// (pccp.toml) so an operator can change them without redeploying:
// SIGHUP (or Reload()) re-reads the file atomically. Environment
// variables still override the file per key (12-factor deployments
// keep working); precedence is env > file > default.
//
// Lookup order for the file: $PCCP_CONFIG, ./pccp.toml,
// /etc/pccp/pccp.toml — first existing file wins; none is not an
// error (defaults apply).
package config

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/BurntSushi/toml"
)

// Runtime is the reloadable runtime configuration surface.
type Runtime struct {
	// Relay is the cross-process admin channel: where the control
	// plane reaches the relay's admin API (directive delivery,
	// broadcasts, catalog push). Empty = the channel is unconfigured
	// and dependent actions fail honestly.
	RelayAdminURL string `toml:"relay_admin_url"`
	// Probes: component addresses the SRE console TCP-dials.
	RelayProbeAddr string `toml:"relay_probe_addr"`
	PIAProbeAddr   string `toml:"pia_probe_addr"`
	// Governance is the relay-side deployment floor advertised in
	// HELLO_ACK (D5); connectors below it refuse the handshake.
	MinHarnessVersion string `toml:"min_harness_version"`
	// Sessions: TTL overrides (seconds); 0 keeps the 8h/30m defaults.
	SessionTTLSeconds int `toml:"session_ttl_seconds"`
	IdleTTLSeconds    int `toml:"idle_ttl_seconds"`
	// WebUI: origins allowed to use the dari.web/1 carrier (relay).
	WebOriginAllowlist []string `toml:"web_origin_allowlist"`
}

// defaults for every field (the floor when neither file nor env set it).
func defaultRuntime() Runtime {
	return Runtime{}
}

var runtimeSnapshot atomic.Pointer[Runtime]

// current returns the live snapshot, initializing on first access.
func current() Runtime {
	if s := runtimeSnapshot.Load(); s != nil {
		return *s
	}
	Reload()
	if s := runtimeSnapshot.Load(); s != nil {
		return *s
	}
	return defaultRuntime()
}

// configPaths is the file lookup order.
func configPaths() []string {
	paths := []string{}
	if p := os.Getenv("PCCP_CONFIG"); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, "pccp.toml", "/etc/pccp/pccp.toml")
	return paths
}

// Reload re-reads the configuration file (first existing path) and
// atomically swaps the live snapshot. A missing file resets to
// defaults+env; a malformed file KEEPS the previous snapshot and
// returns the error — a bad edit never takes the process down.
func Reload() error {
	rt := defaultRuntime()
	for _, p := range configPaths() {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if _, err := toml.DecodeFile(p, &rt); err != nil {
			return err
		}
		break
	}
	// Env overrides per key.
	if v := os.Getenv("PCCP_RELAY_ADMIN_URL"); v != "" {
		rt.RelayAdminURL = v
	}
	if v := os.Getenv("PCCP_RELAY_PROBE_ADDR"); v != "" {
		rt.RelayProbeAddr = v
	}
	if v := os.Getenv("PCCP_PIA_PROBE_ADDR"); v != "" {
		rt.PIAProbeAddr = v
	}
	if v := os.Getenv("PCCP_MIN_HARNESS_VERSION"); v != "" {
		rt.MinHarnessVersion = v
	}
	if v := os.Getenv("PCCP_SESSION_TTL_SECONDS"); v != "" {
		if n := atoiPositive(v); n > 0 {
			rt.SessionTTLSeconds = n
		}
	}
	if v := os.Getenv("PCCP_IDLE_TTL_SECONDS"); v != "" {
		if n := atoiPositive(v); n > 0 {
			rt.IdleTTLSeconds = n
		}
	}
	if v := os.Getenv("DARI_WEB_ORIGIN_ALLOWLIST"); v != "" {
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				rt.WebOriginAllowlist = append(rt.WebOriginAllowlist, o)
			}
		}
	}
	runtimeSnapshot.Store(&rt)
	return nil
}

// ListenForReload reloads on SIGHUP until stop closes. Call once per
// process (main); log-only on errors so a bad edit is visible.
func ListenForReload(stop <-chan struct{}) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-stop:
				signal.Stop(ch)
				return
			case <-ch:
				if err := Reload(); err != nil {
					log.Printf("config: reload FAILED (keeping previous settings): %v", err)
				} else {
					log.Printf("config: reloaded")
				}
			}
		}
	}()
}

// ---- typed getters (the read surface call sites use) ----

// RelayAdminURL is the relay admin channel base ("" = unconfigured).
func RelayAdminURL() string { return current().RelayAdminURL }

// RelayProbeAddr / PIAProbeAddr are the SRE probe targets.
func RelayProbeAddr() string { return current().RelayProbeAddr }
func PIAProbeAddr() string   { return current().PIAProbeAddr }

// MinHarnessVersion is the deployment-wide connector floor (D5).
func MinHarnessVersion() string { return current().MinHarnessVersion }

// SessionTTLs returns the session + idle TTL seconds (defaults 8h/30m
// when unset or invalid).
func SessionTTLs() (session, idle int) {
	rt := current()
	session, idle = 8*3600, 1800
	if rt.SessionTTLSeconds > 0 {
		session = rt.SessionTTLSeconds
	}
	if rt.IdleTTLSeconds > 0 {
		idle = rt.IdleTTLSeconds
	}
	return session, idle
}

// WebOriginAllowlist returns the allowed browser origins (dari.web/1).
func WebOriginAllowlist() []string {
	return append([]string(nil), current().WebOriginAllowlist...)
}

func atoiPositive(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 0
		}
	}
	return n
}
