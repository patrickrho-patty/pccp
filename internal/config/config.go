package config

import (
	"fmt"
	"os"
	"strings"
)

// ServerConfig holds control plane server configuration.
type ServerConfig struct {
	// HTTP server
	HTTPAddr string `json:"http_addr"`
	// Database
	DBDriver string `json:"db_driver"`
	DBDSN    string `json:"db_dsn"`
	// JWT
	JWTSecret string `json:"jwt_secret"`
	// Control Plane CA
	CAKeyFile  string `json:"ca_key_file"`
	CACertFile string `json:"ca_cert_file"`
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
	QUICAddr string `json:"quic_addr"`
	TCPAddr  string `json:"tcp_addr"`
	// TLS
	TLSCertFile string `json:"tls_cert_file"`
	TLSKeyFile  string `json:"tls_key_file"`
	// Control Plane API
	ControlPlaneURL   string `json:"control_plane_url"`
	ControlPlaneToken string `json:"control_plane_token"`
	// Database (shared or relay-local)
	DBDriver string `json:"db_driver"`
	DBDSN    string `json:"db_dsn"`
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
	TLSCertFile string `json:"tls_cert_file"`
	TLSKeyFile  string `json:"tls_key_file"`
	// Local serving engine
	ServingEngineType string `json:"serving_engine_type"`  // vllm, sglang, mock
	ServingEngineURL  string `json:"serving_engine_url"` // localhost URL
	// Model
	ModelPackageID   string `json:"model_package_id"`
	ModelWeightsPath string `json:"model_weights_path"`
	// Attestation
	AssuranceLevel      string `json:"assurance_level"`
	AttestationInterval string `json:"attestation_interval"` // duration string
	// Database
	DBDriver string `json:"db_driver"`
	DBDSN    string `json:"db_dsn"`
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
		DBDriver:            getenvDefault("PCCP_DB_DRIVER", "sqlite"),
		DBDSN:               os.Getenv("PCCP_DB_DSN"),
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
