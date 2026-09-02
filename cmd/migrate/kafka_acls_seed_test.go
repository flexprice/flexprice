package main

import (
	"testing"

	"github.com/Shopify/sarama"
	"github.com/stretchr/testify/assert"
)

func TestSeedACLsEnabled_OnlySCRAM(t *testing.T) {
	assert.True(t, seedACLsEnabled(true, sarama.SASLTypeSCRAMSHA256))
	assert.True(t, seedACLsEnabled(true, sarama.SASLTypeSCRAMSHA512))
	// OAUTHBEARER (GCP) must NOT seed — even though UseSASL is true there.
	assert.False(t, seedACLsEnabled(true, sarama.SASLTypeOAuth))
	// Plaintext / PLAIN dev must NOT seed.
	assert.False(t, seedACLsEnabled(true, sarama.SASLTypePlaintext))
	assert.False(t, seedACLsEnabled(true, ""))
	// SCRAM mechanism but SASL disabled — unauthenticated admin, must NOT seed.
	assert.False(t, seedACLsEnabled(false, sarama.SASLTypeSCRAMSHA256))
	assert.False(t, seedACLsEnabled(false, sarama.SASLTypeSCRAMSHA512))
}

// The --seed-acls outer switch off returns nil before touching the (nil) admin.
func TestSeedAllowAllACLs_FlagOffIsNoop(t *testing.T) {
	assert.NoError(t, seedAllowAllACLs(nil, false, true, sarama.SASLTypeSCRAMSHA256, false))
}
