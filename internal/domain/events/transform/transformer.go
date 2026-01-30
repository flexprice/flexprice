package transform

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// BentoInput represents the structure of a Bento billing entry
type BentoInput struct {
	OrgID          string                 `json:"orgId"`
	MethodName     string                 `json:"methodName"`
	ProviderName   string                 `json:"providerName"`
	ServiceName    string                 `json:"serviceName"`
	Data           map[string]interface{} `json:"data"`
	ID             string                 `json:"id"`
	CreatedAt      string                 `json:"createdAt"`
	TargetItemID   string                 `json:"targetItemId"`
	BYOK           interface{}            `json:"byok"`
	DataInterface  string                 `json:"dataInterface"`
	ReferenceCost  interface{}            `json:"referenceCost"`
	TargetItemType string                 `json:"targetItemType"`
	ReferenceType  string                 `json:"referenceType"`
	ReferenceID    string                 `json:"referenceId"`
	StartedAt      string                 `json:"startedAt"`
	UpdatedAt      string                 `json:"updatedAt"`
	EndedAt        string                 `json:"endedAt"`
}

// TransformBentoToEvent transforms a Bento billing entry payload to an Event
// Returns nil if the event is invalid and should be skipped
func TransformBentoToEvent(payload string, tenantID, environmentID string) (*events.Event, error) {
	// Parse the payload
	var input BentoInput
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse Bento payload").
			Mark(ierr.ErrValidation)
	}

	// Validation (first mapping in Bento)
	if !isValidBentoInput(&input) {
		// Invalid input - skip this event
		return nil, nil
	}

	// Extract properties from data
	properties := make(map[string]interface{})
	if input.Data != nil {
		for key, value := range input.Data {
			// If value is an object with key/value, extract value
			if valueMap, ok := value.(map[string]interface{}); ok {
				if val, exists := valueMap["value"]; exists {
					properties[key] = toString(val)
				} else {
					properties[key] = toString(value)
				}
			} else {
				properties[key] = toString(value)
			}
		}
	}

	// Compute resolved names
	resolvedProviderName := ""
	if input.ProviderName != "" {
		resolvedProviderName = strings.ToLower(strings.TrimSpace(input.ProviderName))
	} else if input.ServiceName != "" {
		resolvedProviderName = strings.ToLower(strings.TrimSpace(input.ServiceName))
	}

	resolvedModelName := ""
	if modelName, ok := properties["modelName"].(string); ok && modelName != "" {
		resolvedModelName = "-" + strings.ToLower(strings.TrimSpace(modelName))
	} else if input.Data != nil {
		if modelNameVal, exists := input.Data["modelName"]; exists {
			if modelNameStr := toString(modelNameVal); modelNameStr != "" {
				resolvedModelName = "-" + strings.ToLower(strings.TrimSpace(modelNameStr))
			}
		}
	}

	resolvedMethodName := ""
	if input.MethodName != "" {
		if input.MethodName == "BEDROCK_LLM" {
			resolvedMethodName = "-modeling"
		} else {
			resolvedMethodName = "-" + strings.ToLower(strings.TrimSpace(input.MethodName))
		}
	}

	// Store resolved names in properties
	properties["resolvedProviderName"] = resolvedProviderName
	properties["resolvedModelName"] = resolvedModelName
	properties["resolvedMethodName"] = resolvedMethodName

	// Add optional top-level fields to properties
	if input.BYOK != nil {
		properties["byok"] = toString(input.BYOK)
	}
	if input.DataInterface != "" {
		properties["dataInterface"] = input.DataInterface
	}
	if input.ServiceName != "" {
		properties["serviceName"] = input.ServiceName
	}
	if input.ProviderName != "" {
		properties["providerName"] = input.ProviderName
	}
	if input.ReferenceCost != nil {
		properties["referenceCost"] = toString(input.ReferenceCost)
	}
	if input.MethodName != "" {
		properties["methodName"] = input.MethodName
	}
	if input.TargetItemType != "" {
		properties["targetItemType"] = input.TargetItemType
	}
	if input.TargetItemID != "" {
		properties["targetItemId"] = input.TargetItemID
	}
	if input.ReferenceType != "" {
		properties["referenceType"] = input.ReferenceType
	}
	if input.ReferenceID != "" {
		properties["referenceId"] = input.ReferenceID
	}
	if input.StartedAt != "" {
		properties["startedAt"] = input.StartedAt
	}
	if input.CreatedAt != "" {
		properties["createdAt"] = input.CreatedAt
	}
	if input.UpdatedAt != "" {
		properties["updatedAt"] = input.UpdatedAt
	}
	if input.EndedAt != "" {
		properties["endedAt"] = input.EndedAt
	}

	// Compute billable_value and billable_unit
	billableValue := ""
	billableUnit := ""

	if numChars, ok := properties["numCharacters"]; ok && numChars != nil {
		billableValue = toString(numChars)
		billableUnit = "characters"
	} else if durationMS, ok := properties["durationMS"]; ok && durationMS != nil {
		billableValue = toString(durationMS)
		billableUnit = "milliseconds"
	}

	// Multiply by channels if both exist and are valid numbers > 0
	if billableValue != "" {
		if channels, ok := properties["channels"]; ok && channels != nil {
			channelsFloat, err1 := toFloat(channels)
			billableFloat, err2 := toFloat(billableValue)
			if err1 == nil && err2 == nil && channelsFloat > 0 && billableFloat > 0 {
				billableValue = fmt.Sprintf("%f", billableFloat*channelsFloat)
			}
		}
	}

	properties["billable_value"] = billableValue
	properties["billable_unit"] = billableUnit

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, input.CreatedAt)
	if err != nil {
		// Try alternative formats
		timestamp, err = time.Parse(time.RFC3339Nano, input.CreatedAt)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to parse createdAt timestamp").
				Mark(ierr.ErrValidation)
		}
	}

	// Build the event
	eventName := resolvedProviderName + resolvedMethodName + resolvedModelName
	source := input.TargetItemID

	event := &events.Event{
		ID:                 input.ID,
		TenantID:           tenantID,
		EnvironmentID:      environmentID,
		ExternalCustomerID: input.OrgID,
		EventName:          eventName,
		Properties:         properties,
		Source:             source,
		Timestamp:          timestamp,
	}

	return event, nil
}

// TransformBentoBatch transforms a batch of raw events to events
// Skips invalid events and returns only valid ones
func TransformBentoBatch(rawEvents []*events.RawEvent) ([]*events.Event, error) {
	var transformedEvents []*events.Event
	var errors []error

	for _, rawEvent := range rawEvents {
		event, err := TransformBentoToEvent(rawEvent.Payload, rawEvent.TenantID, rawEvent.EnvironmentID)
		if err != nil {
			// Log error but continue processing other events
			errors = append(errors, err)
			continue
		}
		if event != nil {
			// Valid event
			transformedEvents = append(transformedEvents, event)
		}
		// If event is nil, it was invalid and should be skipped
	}

	// Return the transformed events even if some failed
	// The caller can decide how to handle partial failures
	return transformedEvents, nil
}

// isValidBentoInput validates the Bento input according to the first mapping
func isValidBentoInput(input *BentoInput) bool {
	// Check required fields
	if input.OrgID == "" {
		return false
	}
	if input.MethodName == "" {
		return false
	}
	// Must have either providerName or serviceName
	if input.ProviderName == "" && input.ServiceName == "" {
		return false
	}
	// Must have data.modelName
	if input.Data == nil {
		return false
	}
	modelName, exists := input.Data["modelName"]
	if !exists || toString(modelName) == "" {
		return false
	}
	if input.ID == "" {
		return false
	}
	if input.CreatedAt == "" {
		return false
	}

	return true
}

// toString converts any value to string
func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", val)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%v", val)
	case bool:
		return fmt.Sprintf("%t", val)
	default:
		// For complex types, marshal to JSON
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}

// toFloat converts any value to float64
func toFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
