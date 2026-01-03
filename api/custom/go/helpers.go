package gosdk

import (
	"fmt"
	"math/rand"
	"regexp"
	"time"
)

// CustomHelpers provides utility functions for the FlexPrice Go SDK
type CustomHelpers struct{}

// FormatCurrency formats currency amount with proper formatting
func (h *CustomHelpers) FormatCurrency(amount float64, currency string) string {
	if currency == "USD" {
		return fmt.Sprintf("$%.2f", amount)
	}
	return fmt.Sprintf("%.2f %s", amount, currency)
}

// GenerateID generates a unique ID with optional prefix
func (h *CustomHelpers) GenerateID(prefix string) string {
	if prefix == "" {
		prefix = "id"
	}
	timestamp := time.Now().UnixNano() / int64(time.Millisecond)
	randomStr := generateRandomString(9)
	return fmt.Sprintf("%s_%d_%s", prefix, timestamp, randomStr)
}

// IsValidEmail validates email format
func (h *CustomHelpers) IsValidEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	return emailRegex.MatchString(email)
}

// FormatDate formats date to ISO string
func (h *CustomHelpers) FormatDate(t time.Time) string {
	return t.Format(time.RFC3339)
}

// generateRandomString generates a random string of specified length
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// NewCustomHelpers creates a new instance of CustomHelpers
func NewCustomHelpers() *CustomHelpers {
	return &CustomHelpers{}
}
