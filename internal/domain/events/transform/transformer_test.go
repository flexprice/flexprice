package transform

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testTenantID      = "tenant_test"
	testEnvironmentID = "env_test"
)

// buildPayload marshals the given map to a JSON string for use as a Bento payload.
func buildPayload(t *testing.T, fields map[string]interface{}) string {
	t.Helper()
	b, err := json.Marshal(fields)
	require.NoError(t, err)
	return string(b)
}

// validBase returns a minimal valid BentoInput field map. Callers can add or
// override fields before passing to buildPayload.
func validBase() map[string]interface{} {
	return map[string]interface{}{
		"id":             "019d0db1-9ce4-7112-aeb1-a7dc08bf96fb",
		"orgId":          "org-test-001",
		"methodName":     "modeling",
		"providerName":   "openai",
		"createdAt":      "2026-03-21T00:00:37.092Z",
		"startedAt":      "2026-03-21T00:00:35.547Z",
		"endedAt":        "2026-03-21T00:00:37.092Z",
		"updatedAt":      "2026-03-21T00:00:37.092Z",
		"targetItemId":   "019d0db0-cba6-7eed-acf9-ae474482bbfb",
		"targetItemType": "call",
		"referenceType":  "completion",
		"referenceCost":  0.001994,
		"dataInterface":  "MODEL_USAGE_DATA_TYPE",
		"byok":           false,
		"data": map[string]interface{}{
			"modelName": "gpt-4.1",
		},
	}
}

// TestTransformBentoToEvent_BillablePromptTokens covers all scenarios for the
// billablePromptTokens derived field.
func TestTransformBentoToEvent_BillablePromptTokens(t *testing.T) {
	tests := []struct {
		name                string
		dataOverride        map[string]interface{}
		wantBillable        string
		wantBillablePresent bool
	}{
		{
			name: "both tokens present — standard case",
			dataOverride: map[string]interface{}{
				"promptTokens":       2961,
				"cachedPromptTokens": 2816,
				"completionTokens":   37,
				"modelName":          "gpt-4.1",
			},
			wantBillable:        "145",
			wantBillablePresent: true,
		},
		{
			name: "promptTokens only, no cachedPromptTokens — cached treated as 0",
			dataOverride: map[string]interface{}{
				"promptTokens":     1000,
				"completionTokens": 20,
				"modelName":        "gpt-4.1",
			},
			wantBillable:        "1000",
			wantBillablePresent: true,
		},
		{
			name: "neither promptTokens nor cachedPromptTokens — field omitted",
			dataOverride: map[string]interface{}{
				"completionTokens": 20,
				"modelName":        "gpt-4.1",
			},
			wantBillablePresent: false,
		},
		{
			name: "cachedPromptTokens present but no promptTokens — field omitted",
			dataOverride: map[string]interface{}{
				"cachedPromptTokens": 500,
				"completionTokens":   20,
				"modelName":          "gpt-4.1",
			},
			wantBillablePresent: false,
		},
		{
			name: "zero cachedPromptTokens",
			dataOverride: map[string]interface{}{
				"promptTokens":       500,
				"cachedPromptTokens": 0,
				"modelName":          "gpt-4.1",
			},
			wantBillable:        "500",
			wantBillablePresent: true,
		},
		{
			name: "both tokens zero",
			dataOverride: map[string]interface{}{
				"promptTokens":       0,
				"cachedPromptTokens": 0,
				"modelName":          "gpt-4.1",
			},
			wantBillable:        "0",
			wantBillablePresent: true,
		},
		{
			name: "cachedPromptTokens exceeds promptTokens — negative result",
			dataOverride: map[string]interface{}{
				"promptTokens":       100,
				"cachedPromptTokens": 200,
				"modelName":          "gpt-4.1",
			},
			wantBillable:        "-100",
			wantBillablePresent: true,
		},
		{
			name: "large token counts",
			dataOverride: map[string]interface{}{
				"promptTokens":       128000,
				"cachedPromptTokens": 64000,
				"modelName":          "gpt-4.1",
			},
			wantBillable:        "64000",
			wantBillablePresent: true,
		},
		{
			name: "string-typed token values in data",
			dataOverride: map[string]interface{}{
				"promptTokens":       "1795",
				"cachedPromptTokens": "1664",
				"modelName":          "gpt-4.1",
			},
			wantBillable:        "131",
			wantBillablePresent: true,
		},
		{
			name: "non-numeric promptTokens — defaults to 0 (toInt64OrZero semantics)",
			dataOverride: map[string]interface{}{
				"promptTokens": "not-a-number",
				"modelName":    "gpt-4.1",
			},
			wantBillable:        "0",
			wantBillablePresent: true,
		},
		{
			name: "nested {value: X} form for tokens",
			dataOverride: map[string]interface{}{
				"promptTokens":       map[string]interface{}{"value": 2961},
				"cachedPromptTokens": map[string]interface{}{"value": 2816},
				"modelName":          "gpt-4.1",
			},
			wantBillable:        "145",
			wantBillablePresent: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := validBase()
			base["data"] = tc.dataOverride
			payload := buildPayload(t, base)

			event, err := TransformBentoToEvent(payload, testTenantID, testEnvironmentID)
			require.NoError(t, err)
			require.NotNil(t, event)

			billable, exists := event.Properties["billablePromptTokens"]
			if !tc.wantBillablePresent {
				assert.False(t, exists, "expected billablePromptTokens to be absent")
				return
			}
			require.True(t, exists, "expected billablePromptTokens to be present")
			assert.Equal(t, tc.wantBillable, billable)
		})
	}
}

// TestTransformBentoToEvent_FullSamplePayload validates the complete
// transformation using the real-world Bento payload format.
func TestTransformBentoToEvent_FullSamplePayload(t *testing.T) {
	payload := `{
		"byok": false,
		"createdAt": "2026-03-21T00:00:37.092Z",
		"data": {
			"cachedPromptTokens": 2816,
			"completionTokens": 37,
			"modelName": "gpt-4.1",
			"promptTokens": 2961
		},
		"dataInterface": "MODEL_USAGE_DATA_TYPE",
		"endedAt": "2026-03-21T00:00:37.092Z",
		"id": "019d0db1-9ce4-7112-aeb1-a7dc08bf96fb",
		"isSubscribed": false,
		"methodName": "modeling",
		"orgId": "org-test-001",
		"providerName": "openai",
		"referenceCost": 0.001994,
		"referenceType": "completion",
		"startedAt": "2026-03-21T00:00:35.547Z",
		"targetItemId": "019d0db0-cba6-7eed-acf9-ae474482bbfb",
		"targetItemType": "call",
		"updatedAt": "2026-03-21T00:00:37.092Z"
	}`

	event, err := TransformBentoToEvent(payload, testTenantID, testEnvironmentID)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, "019d0db1-9ce4-7112-aeb1-a7dc08bf96fb", event.ID)
	assert.Equal(t, "org-test-001", event.ExternalCustomerID)
	assert.Equal(t, "openai-modeling-gpt-4.1", event.EventName)
	assert.Equal(t, testTenantID, event.TenantID)
	assert.Equal(t, testEnvironmentID, event.EnvironmentID)

	props := event.Properties
	assert.Equal(t, "2961", props["promptTokens"])
	assert.Equal(t, "2816", props["cachedPromptTokens"])
	assert.Equal(t, "37", props["completionTokens"])
	assert.Equal(t, "145", props["billablePromptTokens"])
	assert.Equal(t, "openai", props["resolvedProviderName"])
	assert.Equal(t, "-modeling", props["resolvedMethodName"])
	assert.Equal(t, "-gpt-4.1", props["resolvedModelName"])
	assert.Equal(t, "false", props["byok"])
	assert.Equal(t, "MODEL_USAGE_DATA_TYPE", props["dataInterface"])
	assert.Equal(t, "completion", props["referenceType"])
}

// TestTransformBentoToEvent_InvalidInputs covers validation and error paths.
func TestTransformBentoToEvent_InvalidInputs(t *testing.T) {
	noOrgID := validBase()
	delete(noOrgID, "orgId")

	noMethod := validBase()
	delete(noMethod, "methodName")

	noProvider := validBase()
	delete(noProvider, "providerName")

	noID := validBase()
	delete(noID, "id")

	tests := []struct {
		name    string
		payload string
		wantNil bool // event is nil (validation skip)
		wantErr bool // hard parse/transform error
	}{
		{
			name:    "malformed JSON",
			payload: `{not valid json`,
			wantErr: true,
		},
		{
			name:    "missing orgId",
			payload: buildPayload(t, noOrgID),
			wantNil: true,
		},
		{
			name:    "missing methodName",
			payload: buildPayload(t, noMethod),
			wantNil: true,
		},
		{
			name:    "missing providerName and serviceName",
			payload: buildPayload(t, noProvider),
			wantNil: true,
		},
		{
			name:    "missing id",
			payload: buildPayload(t, noID),
			wantNil: true,
		},
		{
			name:    "invalid createdAt timestamp",
			payload: `{"id":"abc","orgId":"org-1","methodName":"modeling","providerName":"openai","createdAt":"not-a-date"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event, err := TransformBentoToEvent(tc.payload, testTenantID, testEnvironmentID)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.wantNil {
				assert.Nil(t, event)
			}
		})
	}
}

// TestTransformBentoToEvent_ResolvedNames validates event name construction
// across different provider/method combinations.
func TestTransformBentoToEvent_ResolvedNames(t *testing.T) {
	tests := []struct {
		name          string
		methodName    string
		providerName  string
		serviceName   string
		dataModelName string
		wantEventName string
		wantResolved  map[string]string
	}{
		{
			name:          "standard openai modeling with model",
			methodName:    "modeling",
			providerName:  "openai",
			dataModelName: "gpt-4.1",
			wantEventName: "openai-modeling-gpt-4.1",
			wantResolved: map[string]string{
				"resolvedProviderName": "openai",
				"resolvedMethodName":   "-modeling",
				"resolvedModelName":    "-gpt-4.1",
			},
		},
		{
			name:          "BEDROCK_LLM method maps to -modeling",
			methodName:    "BEDROCK_LLM",
			providerName:  "aws",
			dataModelName: "claude-3",
			wantEventName: "aws-modeling-claude-3",
			wantResolved: map[string]string{
				"resolvedMethodName": "-modeling",
			},
		},
		{
			name:          "serviceName used when providerName absent",
			methodName:    "transcription",
			serviceName:   "Deepgram",
			dataModelName: "nova-2",
			wantEventName: "deepgram-transcription-nova-2",
			wantResolved: map[string]string{
				"resolvedProviderName": "deepgram",
			},
		},
		{
			name:          "no model — event name has no model suffix",
			methodName:    "modeling",
			providerName:  "openai",
			wantEventName: "openai-modeling",
		},
		{
			name:          "method name lowercased and trimmed",
			methodName:    "  Transcription  ",
			providerName:  "openai",
			wantEventName: "openai-transcription",
			wantResolved: map[string]string{
				"resolvedMethodName": "-transcription",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]interface{}{}
			if tc.dataModelName != "" {
				data["modelName"] = tc.dataModelName
			}

			fields := map[string]interface{}{
				"id":            "test-id-001",
				"orgId":         "org-test",
				"methodName":    tc.methodName,
				"createdAt":     "2026-03-21T00:00:37.092Z",
				"updatedAt":     "2026-03-21T00:00:37.092Z",
				"startedAt":     "2026-03-21T00:00:35.547Z",
				"endedAt":       "2026-03-21T00:00:37.092Z",
				"dataInterface": "MODEL_USAGE_DATA_TYPE",
				"data":          data,
			}
			if tc.providerName != "" {
				fields["providerName"] = tc.providerName
			}
			if tc.serviceName != "" {
				fields["serviceName"] = tc.serviceName
			}

			event, err := TransformBentoToEvent(buildPayload(t, fields), testTenantID, testEnvironmentID)
			require.NoError(t, err)
			require.NotNil(t, event)

			assert.Equal(t, tc.wantEventName, event.EventName)
			for k, v := range tc.wantResolved {
				assert.Equal(t, v, event.Properties[k], "property %s", k)
			}
		})
	}
}

// TestTransformBentoBatch validates batch processing with mixed valid/invalid events.
func TestTransformBentoBatch(t *testing.T) {
	validFields := validBase()
	validFields["data"] = map[string]interface{}{
		"promptTokens":       1000,
		"cachedPromptTokens": 200,
		"modelName":          "gpt-4.1",
	}
	validPayload := buildPayload(t, validFields)

	// orgId empty → skipped (no error)
	skippedFields := validBase()
	skippedFields["orgId"] = ""
	skippedPayload := buildPayload(t, skippedFields)

	rawEvts := []*events.RawEvent{
		{Payload: validPayload, TenantID: testTenantID, EnvironmentID: testEnvironmentID},
		{Payload: `{not json}`, TenantID: testTenantID, EnvironmentID: testEnvironmentID},
		{Payload: skippedPayload, TenantID: testTenantID, EnvironmentID: testEnvironmentID},
		{Payload: validPayload, TenantID: testTenantID, EnvironmentID: testEnvironmentID},
	}

	result, err := TransformBentoBatch(rawEvts)
	// Batch swallows per-event errors and returns partial results
	require.NoError(t, err)
	assert.Len(t, result, 2, "only 2 valid non-skipped events expected")
	for _, e := range result {
		assert.Equal(t, "800", e.Properties["billablePromptTokens"])
	}
}

// Padded keys in the Bento payload must be trimmed before the derived fields are
// computed. Normalizing only at the end produced trimmed stored properties while
// silently skipping billablePromptTokens, the model suffix and billable_value.
func TestTransformBentoToEvent_NormalizesBeforeDerivedFields(t *testing.T) {
	fields := validBase()
	fields["data"] = map[string]interface{}{
		" modelName":          "gpt-4.1",
		"promptTokens ":       "100",
		" cachedPromptTokens": "30",
		"numCharacters ":      "50",
		" channels":           "2",
	}

	event, err := TransformBentoToEvent(buildPayload(t, fields), testTenantID, testEnvironmentID)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, "gpt-4.1", event.Properties["modelName"], "padded key not trimmed")
	assert.Equal(t, "70", event.Properties["billablePromptTokens"], "derived from padded promptTokens")
	assert.Equal(t, "characters", event.Properties["billable_unit"], "derived from padded numCharacters")
	assert.Equal(t, "100.000000", event.Properties["billable_value"], "numCharacters * channels")
	assert.Contains(t, event.EventName, "-gpt-4.1", "model suffix derived from padded modelName")
}
