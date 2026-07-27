// Package keycrypt encrypts recoverable application secrets at rest.
package keycrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const encryptionVersion = 1

// Cipher encrypts persisted secrets with an application-specific key derived from the
// panel JWT secret. Key separation prevents direct reuse of the JWT signing key.
type Cipher struct {
	aead cipher.AEAD
}

// New derives an AES-256-GCM key from the configured application secret.
func New(applicationSecret string) (*Cipher, error) {
	trimmedSecret := strings.TrimSpace(applicationSecret)
	if trimmedSecret == "" {
		return nil, fmt.Errorf("application secret is required")
	}

	keyDerivation := hmac.New(sha256.New, []byte(trimmedSecret))
	_, _ = keyDerivation.Write([]byte("grok-mcp/api-key-encryption/v1"))
	derivedKey := keyDerivation.Sum(nil)

	blockCipher, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("create API key block cipher: %w", err)
	}
	aeadCipher, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return nil, fmt.Errorf("create API key AEAD cipher: %w", err)
	}
	return &Cipher{aead: aeadCipher}, nil
}

// Encrypt returns base64-encoded ciphertext and nonce bound to the provided
// record identity through authenticated additional data.
func (c *Cipher) Encrypt(plaintext, recordIdentity string) (ciphertext string, nonce string, version int, err error) {
	if c == nil || c.aead == nil {
		return "", "", 0, fmt.Errorf("secret encryption is not configured")
	}

	nonceBytes := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		return "", "", 0, fmt.Errorf("generate secret encryption nonce: %w", err)
	}
	ciphertextBytes := c.aead.Seal(nil, nonceBytes, []byte(plaintext), []byte(recordIdentity))
	return base64.RawStdEncoding.EncodeToString(ciphertextBytes), base64.RawStdEncoding.EncodeToString(nonceBytes), encryptionVersion, nil
}

// Decrypt authenticates and decrypts a stored secret.
func (c *Cipher) Decrypt(ciphertext, nonce, recordIdentity string, version int) (string, error) {
	if c == nil || c.aead == nil {
		return "", fmt.Errorf("secret encryption is not configured")
	}
	if version != encryptionVersion {
		return "", fmt.Errorf("unsupported secret encryption version %d", version)
	}

	ciphertextBytes, err := base64.RawStdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode secret ciphertext: %w", err)
	}
	nonceBytes, err := base64.RawStdEncoding.DecodeString(nonce)
	if err != nil {
		return "", fmt.Errorf("decode secret nonce: %w", err)
	}
	if len(nonceBytes) != c.aead.NonceSize() {
		return "", fmt.Errorf("invalid secret nonce length")
	}

	plaintextBytes, err := c.aead.Open(nil, nonceBytes, ciphertextBytes, []byte(recordIdentity))
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plaintextBytes), nil
}
