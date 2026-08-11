package config

import (
	"fmt"
	"os"
	"strings"
)

// ServerConfig holds control plane server configuration.
type ServerConfig struct {
	// HTTP server
	HTTPAddr string
	// Database
	DBDriver string
	DBDSN    string
	// JWT
	JWTSecret string
	// Control Plane CA
	CAKeyFile string
	CACertFile string
	// Admin bootstrap
	AdminEmail string
	AdminPassword string
	// File storage (for evidence bundles, diffs, etc.)
	StorageDir string
	// Default language/locale
	DefaultLocale string
	// Deployment profile
	DeploymentProfile string // enterprise, public, sovereign
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
	QUICAddr string
	TCPAddr  string
	// TLS
	TLSCertFile string
	TLSKeyFile  string
	// Control Plane API
	ControlPlaneURL string
	ControlPlaneToken string
	// Database (shared or relay-local)
	DBDriver string
	DBDSN    string
	// Evidence storage
	EvidenceDir string
}

// LoadRelayFromEnv loads relay config from env.
func LoadRelayFromEnv() RelayConfig {
	return RelayConfig{
		QUICAddr:         getenvDefault("PCCP_RELAY_QUIC_ADDR", ":8443"),
		TCPAddr:          getenvDefault("PCCP_RELAY_TCP_ADDR", ":8444"),
		TLSCertFile:      getenvDefault("PCCP_RELAY_TLS_CERT", ".keys/relay.crt"),
		TLSKeyFile:       getenvDefault("PCCP_RELAY_TLS_KEY", ".keys/relay.key"),
		ControlPlaneURL:  getenvDefault("PCCP_CP_URL", "http://localhost:8080"),
		ControlPlaneToken: os.Getenv("PCCP_CP_TOKEN"),
		DBDriver:         getenvDefault("PCCP_DB_DRIVER", "sqlite"),
		DBDSN:            os.Getenv("PCCP_DB_DSN"),
		EvidenceDir:      getenvDefault("PCCP_EVIDENCE_DIR", ".data/evidence"),
	}
}

// PIAConfig holds PIA (Patty Inference Agent) configuration.
type PIAConfig struct {
	// PAPER peer
	PeerID string
	// Relay endpoint
	RelayAddr string
	// TLS
	TLSCertFile string
	TLSKeyFile  string
	// Local serving engine
	ServingEngineType string // vllm, sglang, mock
	ServingEngineURL  string // localhost URL
	// Model
	ModelPackageID string
	ModelWeightsPath string
	// Attestation
	AssuranceLevel string
	AttestationInterval string // duration string
	// Database
	DBDriver string
	DBDSN    string
}

// LoadPIAFromEnv loads PIA config from env.
func LoadPIAFromEnv() PIAConfig {
	return PIAConfig{
		PeerID:             getenvDefault("PCCP_PIA_PEER_ID", "pia-local"),
		RelayAddr:          getenvDefault("PCCP_PIA_RELAY_ADDR", "localhost:8443"),
		TLSCertFile:        getenvDefault("PCCP_PIA_TLS_CERT", ".keys/pia.crt"),
		TLSKeyFile:         getenvDefault("PCCP_PIA_TLS_KEY", ".keys/pia.key"),
		ServingEngineType:  getenvDefault("PCCP_PIA_ENGINE", "mock"),
		ServingEngineURL:   getenvDefault("PCCP_PIA_SERVING_URL", "http://localhost:8081"),
		ModelPackageID:     getenvDefault("PCCP_PIA_MODEL_PACKAGE", ""),
		ModelWeightsPath:   os.Getenv("PCCP_PIA_MODEL_PATH"),
		AssuranceLevel:     getenvDefault("PCCP_PIA_ASSURANCE", "L1"),
		AttestationInterval: getenvDefault("PCCP_PIA_ATTEST_INTERVAL", "5m"),
		DBDriver:           getenvDefault("PCCP_DB_DRIVER", "sqlite"),
		DBDSN:              os.Getenv("PCCP_DB_DSN"),
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
