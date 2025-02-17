package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Scope represents the scope of idempotency
type Scope string

const (
	ScopeSubscriptionInvoice Scope = "subscription_invoice"
	ScopeOneOffInvoice       Scope = "one_off_invoice"

	// Payment
	ScopePayment Scope = "payment"
)

// Generator generates idempotency keys
type Generator struct{}

// NewGenerator creates a new idempotency key generator
func NewGenerator() *Generator {
	return &Generator{}
}

// GenerateKey generates an idempotency key from a scope and parameters
func (g *Generator) GenerateKey(scope Scope, params map[string]interface{}) string {
	// Sort params for consistent hashing
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build hash input
	var b strings.Builder
	b.WriteString(string(scope))
	for _, k := range keys {
		b.WriteString(fmt.Sprintf(":%s=%v", k, params[k]))
	}

	// Generate SHA-256 hash
	hash := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%s-%s", scope, hex.EncodeToString(hash[:8])) // First 8 bytes for readability
}

// ValidateKey validates if an idempotency key matches expected parameters
func (g *Generator) ValidateKey(scope Scope, params map[string]interface{}, key string) bool {
	generated := g.GenerateKey(scope, params)
	return generated == key
}
