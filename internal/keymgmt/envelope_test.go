package keymgmt

import (
	"bytes"
	"testing"
)

// TestEnvelopeRoundTripAndTamper covers Task 16's KMS/HSM seam:
// envelope encryption round-trips, tampering fails closed, and KEK
// identity is enforced.
func TestEnvelopeRoundTripAndTamper(t *testing.T) {
	kek := bytes.Repeat([]byte{7}, 32)
	p1, err := NewLocalProvider(kek, "kek-1")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("customer-secret-material")
	env, err := Seal(p1, secret)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(p1, env)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("round trip failed: %v", err)
	}

	// Tampered ciphertext fails closed (GCM).
	tampered := *env
	tampered.Ciphertext = append([]byte(nil), env.Ciphertext...)
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 0xFF
	if _, err := Open(p1, &tampered); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}

	// Tampered wrapped DEK fails closed.
	t2 := *env
	t2.WrappedDEK = append([]byte(nil), env.WrappedDEK...)
	t2.WrappedDEK[0] ^= 0xFF
	if _, err := Open(p1, &t2); err == nil {
		t.Fatal("tampered DEK unwrapped")
	}

	// A different provider (KEK id) refuses the envelope.
	p2, err := NewLocalProvider(bytes.Repeat([]byte{9}, 32), "kek-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p2, env); err == nil {
		t.Fatal("foreign KEK accepted an envelope")
	}

	// Bad KEK size rejected.
	if _, err := NewLocalProvider(bytes.Repeat([]byte{1}, 16), "short"); err == nil {
		t.Fatal("short KEK accepted")
	}
}
