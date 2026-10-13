package types

import (
	"database/sql/driver"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// Metadata represents a JSONB field for storing key-value pairs
type Metadata map[string]string

// Scan implements the sql.Scanner interface for Metadata
func (m *Metadata) Scan(value interface{}) error {
	if value == nil {
		*m = make(Metadata)
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return ierr.NewError("failed to unmarshal JSONB value").
			WithHint("Please provide a valid JSON value").
			Mark(ierr.ErrValidation)
	}

	result := make(Metadata)
	err := json.Unmarshal(bytes, &result)
	*m = result
	return err
}

// Value implements the driver.Valuer interface for Metadata
func (m Metadata) Value() (driver.Value, error) {
	if m == nil {
		return json.Marshal(make(Metadata))
	}
	return json.Marshal(m)
}

// Invoice / invoice-line-item metadata keys written by the billing engine and
// read back by revenue-facts decomposition and the FINAL flip. Both sides must
// agree on the exact string, so they are constants rather than literals.
const (
	MetadataKeyIsCommitmentTrueup = "is_commitment_trueup"
	MetadataKeyIsOverage          = "is_overage"
	MetadataKeyOverageFactor      = "overage_factor"
	MetadataKeyCommitmentAmount   = "commitment_amount"
	MetadataKeyCommitmentUtilized = "commitment_utilized"
	MetadataKeyUsageResetPeriod   = "usage_reset_period"
	MetadataKeyIsPreview          = "is_preview"
	MetadataKeyDescription        = "description"

	// MetadataValueTrue is the canonical truthy value for the boolean flags above.
	MetadataValueTrue = "true"
)

// GetBool nil-safely reports whether key holds the canonical truthy value.
func (m Metadata) GetBool(key string) bool {
	if m == nil {
		return false
	}
	return m[key] == MetadataValueTrue
}
