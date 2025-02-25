package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// EncryptionService defines the interface for encryption and hashing operations
type EncryptionService interface {
	// Encrypt encrypts plaintext using AES-GCM
	Encrypt(plaintext string) (string, error)

	// Decrypt decrypts ciphertext using AES-GCM
	Decrypt(ciphertext string) (string, error)

	// Hash creates a one-way hash of the input value using SHA-256
	Hash(value string) string
}

type aesEncryptionService struct {
	key    []byte
	logger *logger.Logger
}

// NewEncryptionService creates a new encryption service using the master key from config
func NewEncryptionService(cfg *config.Configuration, logger *logger.Logger) (EncryptionService, error) {
	if cfg.Secrets.EncryptionKey == "" {
		return nil, errors.New(errors.ErrCodeSystemError, "master encryption key not configured")
	}

	// Use the auth secret as the master key (in production, this should come from a secure source like KMS)
	key := []byte(cfg.Secrets.EncryptionKey)

	// Ensure the key is exactly 32 bytes (256 bits) for AES-256
	if len(key) != 32 {
		// If not 32 bytes, hash it to get a consistent 32-byte key
		hasher := sha256.New()
		hasher.Write(key)
		key = hasher.Sum(nil)
	}

	return &aesEncryptionService{
		key:    key,
		logger: logger,
	}, nil
}

// Encrypt encrypts plaintext using AES-GCM and returns base64-encoded ciphertext
func (s *aesEncryptionService) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	// Create a new AES cipher block
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", errors.Wrap(err, errors.ErrCodeSystemError, "failed to create cipher block")
	}

	// Create a new GCM cipher mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.Wrap(err, errors.ErrCodeSystemError, "failed to create GCM")
	}

	// Create a nonce (number used once)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", errors.Wrap(err, errors.ErrCodeSystemError, "failed to generate nonce")
	}

	// Encrypt and authenticate the plaintext
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Encode the result as base64 for storage
	encoded := base64.StdEncoding.EncodeToString(ciphertext)

	return encoded, nil
}

// Decrypt decrypts base64-encoded ciphertext using AES-GCM
func (s *aesEncryptionService) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	// Decode the base64-encoded ciphertext
	decoded, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", errors.Wrap(err, errors.ErrCodeSystemError, "failed to decode ciphertext")
	}

	// Create a new AES cipher block
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", errors.Wrap(err, errors.ErrCodeSystemError, "failed to create cipher block")
	}

	// Create a new GCM cipher mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.Wrap(err, errors.ErrCodeSystemError, "failed to create GCM")
	}

	// Extract the nonce from the ciphertext
	nonceSize := gcm.NonceSize()
	if len(decoded) < nonceSize {
		return "", errors.New(errors.ErrCodeSystemError, "ciphertext too short")
	}

	nonce, ciphertextBytes := decoded[:nonceSize], decoded[nonceSize:]

	// Decrypt and verify the ciphertext
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", errors.Wrap(err, errors.ErrCodeSystemError, "failed to decrypt ciphertext")
	}

	return string(plaintext), nil
}

// Hash creates a one-way hash of the input value using SHA-256
func (s *aesEncryptionService) Hash(value string) string {
	if value == "" {
		return ""
	}

	// Create a new SHA-256 hasher
	hasher := sha256.New()

	// Write the value to the hasher
	hasher.Write([]byte(value))

	// Get the hash sum and convert to hex string
	return hex.EncodeToString(hasher.Sum(nil))
}

// GenerateRandomKey generates a random 32-byte key for AES-256
func GenerateRandomKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}
	return hex.EncodeToString(key), nil
}
