package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ServerConfig holds control plane server configuration.
type ServerConfig struct {
	// HTTP server
	HTTPAddr string `json:"h_t_t_p_addr"`
	// Database
	DBDriver string `json:"d_b_driver"`
	DBDSN    string `json:"d_b_d_s_n"`
	// JWT
	JWTSecret string `json:"j_w_t_secret"`
	// Control Plane CA
	CAKeyFile  string `json:"c_a_key_file"`
	CACertFile string `json:"c_a_cert_file"`
	// Admin bootstrap
	AdminEmail    string `json:"admin_email"`
	AdminPassword string `json:"admin_password"`
	// File storage (for evidence bundles, diffs, etc.)
	StorageDir string `json:"storage_dir"`
	// Default language/locale
	DefaultLocale string `json:"default_locale"`
	// Deployment profile
	DeploymentProfile string `json:"deployment_profile"` // enterprise, public, sovereign
}

// LoadFromEnv loads configuration from environment variables with sensible defaults.
func LoadFromEnv() ServerConfig {
	return ServerConfig{
		HTTPAddr:          getenvDefault("PCCP_HTTP_ADDR", ":8080"),
		DBDriver:          getenvDefault("PCCP_DB_DRIVER", "sqlite"),
		DBDSN:             os.Getenv("PCCP_DB_DSN"),
		JWTSecret:         getenvDefault("PCCP_JWT_SECRET", "dev-only-change-in-production"),
		CAKeyFile:         getenvDefault("PCCP_CA_KEY", ".keys/ca.key"),
		CACertFile:        getenvDefault("PCCP_CA_CERT", ".keys/ca.cert"),
		AdminEmail:        getenvDefault("PCCP_ADMIN_EMAIL", "admin@patty.dev"),
		AdminPassword:     getenvDefault("PCCP_ADMIN_PASSWORD", "changeme"),
		StorageDir:        getenvDefault("PCCP_STORAGE_DIR", ".data/storage"),
		DefaultLocale:     getenvDefault("PCCP_DEFAULT_LOCALE", "ko-KR"),
		DeploymentProfile: getenvDefault("PCCP_PROFILE", "enterprise"),
	}
}

// RelayConfig holds Relay (data plane) configuration.
type RelayConfig struct {
	// QUIC/TCP listen address
	QUICAddr string `json:"q_u_i_c_addr"`
	TCPAddr  string `json:"t_c_p_addr"`
	// TLS
	TLSCertFile string `json:"t_l_s_cert_file"`
	TLSKeyFile  string `json:"t_l_s_key_file"`
	// Control Plane API
	ControlPlaneURL   string `json:"control_plane_u_r_l"`
	ControlPlaneToken string `json:"control_plane_token"`
	// Database (shared or relay-local)
	DBDriver string `json:"d_b_driver"`
	DBDSN    string `json:"d_b_d_s_n"`
	// Evidence storage
	EvidenceDir string `json:"evidence_dir"`
}

// LoadRelayFromEnv loads relay config from env.
func LoadRelayFromEnv() RelayConfig {
	return RelayConfig{
		QUICAddr:          getenvDefault("PCCP_RELAY_QUIC_ADDR", ":8443"),
		TCPAddr:           getenvDefault("PCCP_RELAY_TCP_ADDR", ":8444"),
		TLSCertFile:       getenvDefault("PCCP_RELAY_TLS_CERT", ".keys/relay.crt"),
		TLSKeyFile:        getenvDefault("PCCP_RELAY_TLS_KEY", ".keys/relay.key"),
		ControlPlaneURL:   getenvDefault("PCCP_CP_URL", "http://localhost:8080"),
		ControlPlaneToken: os.Getenv("PCCP_CP_TOKEN"),
		DBDriver:          getenvDefault("PCCP_DB_DRIVER", "sqlite"),
		DBDSN:             os.Getenv("PCCP_DB_DSN"),
		EvidenceDir:       getenvDefault("PCCP_EVIDENCE_DIR", ".data/evidence"),
	}
}

// PIAConfig holds PIA (Patty Inference Agent) configuration.
type PIAConfig struct {
	// DARI peer
	PeerID string `json:"peer_id"`
	// Relay endpoint
	RelayAddr string `json:"relay_addr"`
	// TLS
	TLSCertFile string `json:"t_l_s_cert_file"`
	TLSKeyFile  string `json:"t_l_s_key_file"`
	// Local serving engine
	ServingEngineType string `json:"serving_engine_type"`  // vllm, sglang, mock
	ServingEngineURL  string `json:"serving_engine_u_r_l"` // localhost URL
	// Model
	ModelPackageID   string `json:"model_package_id"`
	ModelWeightsPath string `json:"model_weights_path"`
	// Attestation
	AssuranceLevel      string `json:"assurance_level"`
	AttestationInterval string `json:"attestation_interval"` // duration string
	// Worker-agent mode (DARI scheduler S1)
	WorkerMode          bool   `json:"worker_mode"`
	SchedulerAddr       string `json:"scheduler_addr"`
	CredentialFile      string `json:"credential_file"`
	SubjectKeyFile      string `json:"subject_key_file"`
	ConfigEnvelopeFile  string `json:"config_envelope_file"`
	ConfigPublicKeyHex  string `json:"config_public_key_hex"`
	// Database
	DBDriver string `json:"d_b_driver"`
	DBDSN    string `json:"d_b_d_s_n"`
}

// LoadPIAFromEnv loads PIA config from env.
func LoadPIAFromEnv() PIAConfig {
	return PIAConfig{
		PeerID:              getenvDefault("PCCP_PIA_PEER_ID", "pia-local"),
		RelayAddr:           getenvDefault("PCCP_PIA_RELAY_ADDR", "localhost:8443"),
		TLSCertFile:         getenvDefault("PCCP_PIA_TLS_CERT", ".keys/pia.crt"),
		TLSKeyFile:          getenvDefault("PCCP_PIA_TLS_KEY", ".keys/pia.key"),
		ServingEngineType:   getenvDefault("PCCP_PIA_ENGINE", "vllm"),
		ServingEngineURL:    getenvDefault("PCCP_PIA_SERVING_URL", "http://localhost:8081"),
		ModelPackageID:      getenvDefault("PCCP_PIA_MODEL_PACKAGE", ""),
		ModelWeightsPath:    os.Getenv("PCCP_PIA_MODEL_PATH"),
		AssuranceLevel:      getenvDefault("PCCP_PIA_ASSURANCE", "L1"),
		AttestationInterval: getenvDefault("PCCP_PIA_ATTEST_INTERVAL", "5m"),
		WorkerMode:          getenvBool("PCCP_PIA_WORKER_MODE", false),
		SchedulerAddr:       getenvDefault("PCCP_PIA_SCHED_ADDR", "localhost:8445"),
		CredentialFile:      os.Getenv("PCCP_PIA_CREDENTIAL_FILE"),
		SubjectKeyFile:      os.Getenv("PCCP_PIA_SUBJECT_KEY_FILE"),
		ConfigEnvelopeFile:  os.Getenv("PCCP_PIA_CONFIG_FILE"),
		ConfigPublicKeyHex:  os.Getenv("PCCP_PIA_CONFIG_PUBKEY_HEX"),
		DBDriver:            getenvDefault("PCCP_DB_DRIVER", "sqlite"),
		DBDSN:               os.Getenv("PCCP_DB_DSN"),
	}
}

// SchedulerConfig holds pccp-scheduler (fleet registry) configuration.
type SchedulerConfig struct {
	// HTTP admin API (CP read-through)
	HTTPAddr string `json:"h_t_t_p_addr"`
	// DARI worker listener
	DARIAddr string `json:"d_a_r_i_addr"`
	// Trust material (hex-encoded public keys)
	CAIssuerID         string `json:"c_a_issuer_id"`
	CAPublicKeyHex     string `json:"c_a_public_key_hex"`
	ConfigPublicKeyHex string `json:"config_public_key_hex"`
	// Tenant policy (optional; static file in S1)
	PolicyFile string `json:"policy_file"`
	// Leases
	LeaseTTLSeconds   int `json:"lease_t_t_l_seconds"`
	LeaseGraceSeconds int `json:"lease_grace_seconds"`
	// Admin API auth (empty = open, dev only)
	AdminToken string `json:"admin_token"`
}

// LoadSchedulerFromEnv loads scheduler config from environment variables.
func LoadSchedulerFromEnv() SchedulerConfig {
	return SchedulerConfig{
		HTTPAddr:           getenvDefault("PCCP_SCHED_HTTP_ADDR", ":8455"),
		DARIAddr:           getenvDefault("PCCP_SCHED_DARI_ADDR", ":8445"),
		CAIssuerID:         getenvDefault("PCCP_SCHED_CA_ISSUER_ID", "pccp-ca"),
		CAPublicKeyHex:     os.Getenv("PCCP_SCHED_CA_PUBKEY_HEX"),
		ConfigPublicKeyHex: os.Getenv("PCCP_SCHED_CONFIG_PUBKEY_HEX"),
		PolicyFile:         os.Getenv("PCCP_SCHED_POLICY_FILE"),
		LeaseTTLSeconds:    getenvInt("PCCP_SCHED_LEASE_TTL_SECONDS", 30),
		LeaseGraceSeconds:  getenvInt("PCCP_SCHED_LEASE_GRACE_SECONDS", 60),
		AdminToken:         os.Getenv("PCCP_SCHED_ADMIN_TOKEN"),
	}
}

// EnsureStorageDir creates the storage directory if it doesn't exist.
func EnsureStorageDir(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("config: create storage dir %s: %w", path, err)
	}
	return nil
}

func getenvDefault(key, defaultVal string) string {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	return strings.TrimSpace(v)
}

func getenvInt(key string, defaultVal int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return defaultVal
	}
	return n
}

func getenvBool(key string, defaultVal bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
}
