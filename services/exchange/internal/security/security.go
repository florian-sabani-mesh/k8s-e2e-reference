// Package security holds the correctness-sensitive cryptographic primitives used by
// the exchange service: authenticated encryption for OAuth tokens at rest, PKCE
// generation, unguessable single-use OAuth state, and stable request hashing.
//
// None of these functions log their inputs. Callers must never log verifiers,
// tokens, secrets or 2FA codes either.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Cipher provides AES-256-GCM authenticated encryption for values stored at rest.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a base64 (standard or url) encoded 32-byte key.
// A 32-byte key selects AES-256. The key comes from a Kubernetes Secret, never code.
func NewCipher(encodedKey string) (*Cipher, error) {
	key, err := decodeBase64(strings.TrimSpace(encodedKey))
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt returns base64url(nonce || ciphertext || tag). A fresh random nonce is
// used for every call, so encrypting the same plaintext yields different output.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt and authenticates the ciphertext; tampering fails.
func (c *Cipher) Decrypt(token string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(raw) < c.aead.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, body := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

// RandomState returns 32 bytes of cryptographically secure randomness, base64url
// encoded without padding. It is the raw OAuth state handed to the browser; only
// its hash is persisted.
func RandomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashState returns the hex SHA-256 of raw state. Persisting the hash means a
// database leak does not expose usable state values.
func HashState(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// PKCE returns a fresh (code_verifier, code_challenge) pair using S256:
// verifier = base64url(32 random bytes); challenge = base64url(SHA256(verifier)),
// both without padding, per RFC 7636.
func PKCE() (verifier string, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// RequestHash produces a stable hex digest over the business fields of a request.
// A newline separator that cannot appear in the normalized fields keeps the
// mapping injective, so distinct requests cannot collide onto one hash.
func RequestHash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func decodeBase64(value string) ([]byte, error) {
	if key, err := base64.StdEncoding.DecodeString(value); err == nil {
		return key, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
}
