package moyasar

// Connection metadata keys used across the Moyasar integration.
// Always use these constants instead of raw string literals to prevent typos.
const (
	// Connection metadata keys, stored on the Moyasar connection record's non-encrypted
	// Metadata field (set via POST/PUT /connections {"metadata": {...}}).
	ConnKeySuccessURL = "success_url" // customer redirected here after a successful payment
	ConnKeyCancelURL  = "cancel_url"  // customer redirected here if they cancel/go back
)
