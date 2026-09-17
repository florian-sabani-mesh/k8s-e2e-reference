package security

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

func TestCipherRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	secret := "e2e-coinbase-access-token"
	ciphertext, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ciphertext == secret {
		t.Fatal("ciphertext must differ from plaintext")
	}
	again, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ciphertext == again {
		t.Fatal("nonce reuse: two encryptions produced identical ciphertext")
	}
	plaintext, err := c.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plaintext != secret {
		t.Fatalf("round trip mismatch: got %q", plaintext)
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	c := newTestCipher(t)
	ciphertext, err := c.Encrypt("sensitive")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(ciphertext)
	raw[len(raw)-1] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := c.Decrypt(tampered); err == nil {
		t.Fatal("expected authentication failure on tampered ciphertext")
	}
}

func TestNewCipherRejectsWrongKeyLength(t *testing.T) {
	if _, err := NewCipher(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("expected error for non-32-byte key")
	}
}

func TestPKCEChallengeDerivation(t *testing.T) {
	verifier, challenge, err := PKCE()
	if err != nil {
		t.Fatalf("PKCE: %v", err)
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != want {
		t.Fatalf("challenge is not base64url(SHA256(verifier)): got %q want %q", challenge, want)
	}
	if len(verifier) < 43 {
		t.Fatalf("verifier too short for RFC 7636: %d chars", len(verifier))
	}
}

func TestRandomStateIsUniqueAndHashed(t *testing.T) {
	a, err := RandomState()
	if err != nil {
		t.Fatalf("RandomState: %v", err)
	}
	b, _ := RandomState()
	if a == b {
		t.Fatal("state values must not repeat")
	}
	if HashState(a) == a {
		t.Fatal("stored hash must differ from raw state")
	}
	if HashState(a) != HashState(a) {
		t.Fatal("HashState must be deterministic")
	}
}

func TestRequestHashDistinguishesFields(t *testing.T) {
	base := RequestHash("btc-account", "BTC", "0.25", "addr", "bitcoin")
	if base != RequestHash("btc-account", "BTC", "0.25", "addr", "bitcoin") {
		t.Fatal("RequestHash must be deterministic")
	}
	if base == RequestHash("btc-account", "BTC", "0.50", "addr", "bitcoin") {
		t.Fatal("different amount must change the hash")
	}
	// The separator must prevent field-boundary collisions.
	if RequestHash("a", "b") == RequestHash("ab", "") {
		t.Fatal("field boundaries must be preserved")
	}
}
