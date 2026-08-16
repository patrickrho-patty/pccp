package registry

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// VerifyModelPackage verifies the package's manifest digest + signature
// (web/18 A). Publish and PIA-load paths refuse packages that fail
// (PIA-side verification is enforced by the connector's loader).
func (s *Service) VerifyPackageIntegrity(pkg *models.ModelPackage) error {
	// 1. Recompute the manifest digest from the stored row and compare.
	current := s.buildManifestDigest(pkg)
	if current != pkg.ManifestDigest {
		return fmt.Errorf("manifest digest mismatch (stored %s, computed %s)", pkg.ManifestDigest, current)
	}
	// 2. Verify the signature over the manifest.
	if pkg.Signature == "" || pkg.SignatureKeyID == "" {
		return fmt.Errorf("package signature missing")
	}
	sig, err := hex.DecodeString(pkg.Signature)
	if err != nil {
		return fmt.Errorf("signature decode: %w", err)
	}
	if pkg.SignatureKeyID == s.signingKeyID {
		if !ed25519.Verify(s.signingKey.Public().(ed25519.PublicKey), []byte(current), sig) {
			return fmt.Errorf("signature verification failed")
		}
		return nil
	}
	// A signature from another registered key is verified against that
	// key when the key is known; unknown signers are refused.
	var key models.ServiceSigningKey
	if err := s.db.Where("id = ?", pkg.SignatureKeyID).First(&key).Error; err != nil {
		return fmt.Errorf("signer key %s unknown: %w", pkg.SignatureKeyID, err)
	}
	privBytes, err := hex.DecodeString(key.PrivateHex)
	if err != nil || len(privBytes) != ed25519.PrivateKeySize {
		return fmt.Errorf("signer key material invalid")
	}
	pub := ed25519.PrivateKey(privBytes).Public().(ed25519.PublicKey)
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(current), sig) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}
