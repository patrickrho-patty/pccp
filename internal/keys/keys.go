package keys

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// keys.go provides PERSISTED per-service ed25519 signing identities.
// Before this existed every service generated an ephemeral key at
// boot: signatures broke across restarts (model publishes failed
// verification, receipts could never be verified by a connector that
// had learned a previous key) — an enterprise deployment cannot have
// identity that resets on every deploy.
//
// Keys are created once (load-or-create under service_signing_keys)
// and reused forever. The row holds the private half; production
// deployments with an external KMS should replace this loader — the
// seam is this package alone.

var mu sync.Mutex

// LoadOrCreate returns the persisted private key for the named
// service ("policy-issuer", "provenance-receipts", "registry-publish").
func LoadOrCreate(db *gorm.DB, service string) (ed25519.PrivateKey, error) {
	mu.Lock()
	defer mu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("keys: nil db")
	}
	var row models.ServiceSigningKey
	err := db.Where("service = ?", service).First(&row).Error
	if err == nil {
		raw, derr := hex.DecodeString(row.PrivateHex)
		if derr != nil || len(raw) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("keys: stored key for %s is corrupt", service)
		}
		return ed25519.PrivateKey(raw), nil
	}
	if err.Error() != "record not found" && !isNotFound(err) {
		return nil, fmt.Errorf("keys: load %s: %w", service, err)
	}
	_, priv, gerr := ed25519.GenerateKey(nil)
	if gerr != nil {
		return nil, fmt.Errorf("keys: generate %s: %w", service, gerr)
	}
	if cerr := db.Create(&models.ServiceSigningKey{
		Service:    service,
		PrivateHex: hex.EncodeToString(priv),
	}).Error; cerr != nil {
		return nil, fmt.Errorf("keys: persist %s: %w", service, cerr)
	}
	return priv, nil
}

func isNotFound(err error) bool {
	type notFound interface{ Error() string }
	return err != nil && (err.Error() == "record not found")
}
