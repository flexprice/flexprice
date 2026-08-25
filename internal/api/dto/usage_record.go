package dto

import (
	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	"github.com/flexprice/flexprice/internal/types"
)

// UsageRecordResponse is a usage record as returned by the API, unchanged from the domain model.
type UsageRecordResponse struct {
	*usagerecord.UsageRecord
}

// ListUsageRecordsResponse represents the response for listing usage records.
type ListUsageRecordsResponse = types.ListResponse[*UsageRecordResponse] // @name ListUsageRecordsResponse
