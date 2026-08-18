package keymgmt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const AlertBindingVersion = 1

type AlertSecretContext struct {
	OrganizationID string
	EndpointID     string
	ProviderType   string
}

func (c AlertSecretContext) aad() []byte {
	return []byte("DARI-ALERT-TARGET-v1\x00" + c.OrganizationID + "\x00" + c.EndpointID + "\x00" + strings.ToLower(c.ProviderType))
}

// SealAlertSecret is the single storage codec for alert credentials.
func SealAlertSecret(provider KeyProvider, plaintext string, ctx AlertSecretContext) (encoded, kekID, credentialID string, bindingVersion int, err error) {
	if provider == nil {
		return "", "", "", 0, errors.New("keymgmt: provider_not_configured")
	}
	env, err := SealWithAAD(provider, []byte(plaintext), ctx.aad())
	if err != nil {
		return "", "", "", 0, err
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("keymgmt: encode envelope: %w", err)
	}
	credentialID, err = CredentialID(provider, plaintext)
	if err != nil {
		return "", "", "", 0, err
	}
	return base64.StdEncoding.EncodeToString(raw), env.KEKID, credentialID, AlertBindingVersion, nil
}

// OpenAlertSecret opens either the current row-bound envelope or an explicit
// legacy plaintext/unbound envelope during migration, then verifies its
// persisted audit fingerprint when present.
func OpenAlertSecret(provider KeyProvider, encoded, kekID, legacy string, bindingVersion int, expectedID string, ctx AlertSecretContext) (string, error) {
	return openAlertSecret(provider, encoded, kekID, legacy, bindingVersion, expectedID, ctx, false)
}

// OpenAlertSecretForMigration authenticates the stored envelope while also
// accepting and validating the immediately preceding unkeyed h: identifier.
// Runtime reads never use this compatibility path.
func OpenAlertSecretForMigration(provider KeyProvider, encoded, kekID, legacy string, bindingVersion int, expectedID string, ctx AlertSecretContext) (string, error) {
	return openAlertSecret(provider, encoded, kekID, legacy, bindingVersion, expectedID, ctx, true)
}

func openAlertSecret(provider KeyProvider, encoded, kekID, legacy string, bindingVersion int, expectedID string, ctx AlertSecretContext, migration bool) (string, error) {
	var plaintext string
	var err error
	if encoded == "" {
		plaintext = legacy
	} else if bindingVersion >= AlertBindingVersion {
		plaintext, err = openEncoded(provider, encoded, kekID, ctx.aad())
	} else {
		plaintext, err = OpenEncoded(provider, encoded, kekID)
	}
	if err != nil {
		return "", err
	}
	if plaintext == "" {
		return "", nil
	}
	if strings.HasPrefix(expectedID, "hm:") {
		actualID, fingerprintErr := CredentialID(provider, plaintext)
		if fingerprintErr != nil {
			return "", fingerprintErr
		}
		if actualID != expectedID {
			return "", errors.New("keymgmt: credential_fingerprint_mismatch")
		}
	} else if migration && strings.HasPrefix(expectedID, "h:") {
		if DomainFingerprint("DARI-ALERT-CREDENTIAL-v1", plaintext) != expectedID {
			return "", errors.New("keymgmt: credential_fingerprint_mismatch")
		}
	} else if expectedID != "" && bindingVersion >= AlertBindingVersion {
		return "", errors.New("keymgmt: credential_fingerprint_format_unsupported")
	}
	return plaintext, nil
}
