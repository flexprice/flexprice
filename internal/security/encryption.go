package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// HashResult represents a salted hash result
type HashResult struct {
	Salt string
	Hash string
}

// String implements the Stringer interface and returns just the hash for backward compatibility
func (hr HashResult) String() string {
	return hr.Hash
}

// EncryptionService defines the interface for encryption and hashing operations
type EncryptionService interface {
	// Encrypt encrypts plaintext using AES-GCM
	Encrypt(plaintext string) (string, error)

	// Decrypt decrypts ciphertext using AES-GCM
	Decrypt(ciphertext string) (string, error)

	// Hash creates a salted one-way hash of the input value using SHA-256
	Hash(value string) HashResult

	// VerifyHash verifies if a value matches a previously generated hash with its salt
	VerifyHash(value string, previousHash HashResult) bool
}

type aesEncryptionService struct {
	key    []byte
	logger *logger.Logger
}

// NewEncryptionService creates a new encryption service using the master key from config
func NewEncryptionService(cfg *config.Configuration, logger *logger.Logger) (EncryptionService, error) {
	if cfg.Secrets.EncryptionKey == "" {
		return nil, ierr.NewError("master encryption key not configured").
			WithHint("Master encryption key is not configured").
			Mark(ierr.ErrSystem)
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
		return "", ierr.WithError(err).
			WithHint("Failed to create cipher block").
			Mark(ierr.ErrSystem)
	}

	// Create a new GCM cipher mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create GCM").
			Mark(ierr.ErrSystem)
	}

	// Create a nonce (number used once)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to generate nonce").
			Mark(ierr.ErrSystem)
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
		return "", ierr.WithError(err).
			WithHint("Failed to decode ciphertext").
			Mark(ierr.ErrSystem)
	}

	// Create a new AES cipher block
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create cipher block").
			Mark(ierr.ErrSystem)
	}

	// Create a new GCM cipher mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create GCM").
			Mark(ierr.ErrSystem)
	}

	// Extract the nonce from the ciphertext
	nonceSize := gcm.NonceSize()
	if len(decoded) < nonceSize {
		return "", ierr.NewError("ciphertext too short").
			WithHint("Ciphertext is too short").
			Mark(ierr.ErrSystem)
	}

	nonce, ciphertextBytes := decoded[:nonceSize], decoded[nonceSize:]

	// Decrypt and verify the ciphertext
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to decrypt ciphertext").
			Mark(ierr.ErrSystem)
	}

	return string(plaintext), nil
}

// Hash creates a salted one-way hash of the input value using SHA-256
func (s *aesEncryptionService) Hash(value string) HashResult {
	if value == "" {
		return HashResult{}
	}
}

// VerifyHash verifies if a value matches a previously generated hash with its salt
func (s *aesEncryptionService) VerifyHash(value string, previousHash HashResult) bool {
	if value == "" || previousHash.Salt == "" || previousHash.Hash == "" {
		return false
	}

	// Decode the salt from hex
	salt, err := hex.DecodeString(previousHash.Salt)
	if err != nil {
		s.logger.Error("Failed to decode salt", "error", err)
		return false
	}

	// Create a new SHA-256 hasher
	hasher := sha256.New()

	// Write the salt and then the value to the hasher
	hasher.Write(salt)
	hasher.Write([]byte(value))

	// Compare the computed hash with the stored hash
	computedHash := hex.EncodeToString(hasher.Sum(nil))
	return computedHash == previousHash.Hash

func (s *aesEncryptionService) Hash(value string) HashResult {
	if value == "" {
		return HashResult{}
	}

	// Generate a random 32-byte salt
	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		s.logger.Error("Failed to generate salt", "error", err)
		return HashResult{}
	}

	// Create a new SHA-256 hasher
	hasher := sha256.New()

	// Write the salt and then the value to the hasher
	hasher.Write(salt)
	hasher.Write([]byte(value))

	// Get the hash sum and convert both salt and hash to hex strings
	return HashResult{
		Salt: hex.EncodeToString(salt),
		Hash: hex.EncodeToString(hasher.Sum(nil)),
	}
}
