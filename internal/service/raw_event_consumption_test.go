package service

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	domainSettings "github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

type RawEventConsumptionSuite struct {
	testutil.BaseServiceTestSuite
	svc          *rawEventConsumptionService
	outputPubSub *testutil.InMemoryPubSub
	settingsRepo *testutil.InMemorySettingsStore
}

func TestRawEventConsumptionService(t *testing.T) {
	suite.Run(t, new(RawEventConsumptionSuite))
}

func (s *RawEventConsumptionSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	s.outputPubSub = testutil.NewInMemoryPubSub()
	var ok bool
	s.settingsRepo, ok = s.GetStores().SettingsRepo.(*testutil.InMemorySettingsStore)
	require.True(s.T(), ok, "SettingsRepo must be *testutil.InMemorySettingsStore, got %T", s.GetStores().SettingsRepo)

	params := ServiceParams{
		Logger:       s.GetLogger(),
		Config:       s.GetConfig(),
		DB:           s.GetDB(),
		SettingsRepo: s.settingsRepo,
	}

	s.svc = &rawEventConsumptionService{
		ServiceParams: params,
		outputPubSub:  s.outputPubSub,
		sentryService: sentry.NewSentryService(s.GetConfig(), s.GetLogger()),
	}

	// Default output topic
	s.GetConfig().RawEventConsumption.OutputTopic = testOutputTopic
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	testTenantID      = types.DefaultTenantID
	testEnvironmentID = "env_sandbox"
	testOutputTopic   = "events"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// validBentoPayload returns a minimal valid Bento event JSON for the given orgID.
func validBentoPayload(orgID, eventID string) string {
	return `{"orgId":"` + orgID + `","id":"` + eventID + `","methodName":"CHAT_COMPLETION","providerName":"openai","createdAt":"2024-01-15T10:00:00Z"}`
}

// buildBatchMsg serialises a RawEventBatch into a Watermill message.
func buildBatchMsg(batch RawEventBatch) *message.Message {
	payload, _ := json.Marshal(batch)
	return message.NewMessage("test-uuid", payload)
}

// makeFilterSetting stores an EventIngestionFilterConfig in the in-memory settings
// repo under the test tenant + environment. Fails the test immediately on any error.
func (s *RawEventConsumptionSuite) makeFilterSetting(enabled bool, allowedIDs []string) {
	value, err := json.Marshal(types.EventIngestionFilterConfig{
		Enabled:                    enabled,
		AllowedExternalCustomerIDs: allowedIDs,
	})
	require.NoError(s.T(), err, "marshal filter config")

	var valueMap map[string]interface{}
	require.NoError(s.T(), json.Unmarshal(value, &valueMap), "unmarshal filter config to map")

	setting := &domainSettings.Setting{
		ID:            types.GenerateUUID(),
		Key:           types.SettingKeyEventIngestionFilter,
		Value:         valueMap,
		EnvironmentID: testEnvironmentID,
	}
	setting.TenantID = testTenantID
	setting.Status = types.StatusPublished
	setting.CreatedAt = time.Now()
	setting.UpdatedAt = time.Now()

	ctx := testutil.SetupContext()
	require.NoError(s.T(), s.settingsRepo.Create(ctx, setting), "create filter setting")
}

// publishedExternalIDs decodes all messages published to the output topic and
// returns the sorted list of ExternalCustomerID values.
func (s *RawEventConsumptionSuite) publishedExternalIDs() []string {
	msgs := s.outputPubSub.GetMessages(testOutputTopic)
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		var evt events.Event
		require.NoError(s.T(), json.Unmarshal(m.Payload, &evt),
			"publishedExternalIDs: failed to unmarshal published payload: %s", string(m.Payload))
		ids = append(ids, evt.ExternalCustomerID)
	}
	sort.Strings(ids)
	return ids
}

// ---------------------------------------------------------------------------
// Table-driven filter tests
// ---------------------------------------------------------------------------

func (s *RawEventConsumptionSuite) TestProcessMessage_FilterBehavior() {
	tests := []struct {
		name           string
		setupFilter    func()
		inputOrgIDs    []string // one event per org ID
		wantForwarded  []string // sorted expected ExternalCustomerIDs that reach the output topic
		wantErr        bool
	}{
		{
			name:        "no filter setting → all events forwarded",
			setupFilter: func() { /* no setting created */ },
			inputOrgIDs: []string{"org_001", "org_002", "org_003"},
			wantForwarded: []string{"org_001", "org_002", "org_003"},
		},
		{
			name: "filter enabled=false → all events forwarded regardless of list",
			setupFilter: func() {
				s.makeFilterSetting(false, []string{"org_001"})
			},
			inputOrgIDs:   []string{"org_001", "org_999"},
			wantForwarded: []string{"org_001", "org_999"},
		},
		{
			name: "filter enabled → only allowlisted IDs forwarded",
			setupFilter: func() {
				s.makeFilterSetting(true, []string{"org_001", "org_002"})
			},
			inputOrgIDs:   []string{"org_001", "org_002", "org_003", "org_004"},
			wantForwarded: []string{"org_001", "org_002"},
		},
		{
			name: "filter enabled → no IDs match, nothing forwarded",
			setupFilter: func() {
				s.makeFilterSetting(true, []string{"org_allowed"})
			},
			inputOrgIDs:   []string{"org_not_in_list", "org_also_not"},
			wantForwarded: []string{},
		},
		{
			name: "filter enabled with empty allowlist → everything blocked",
			setupFilter: func() {
				s.makeFilterSetting(true, []string{})
			},
			inputOrgIDs:   []string{"org_001"},
			wantForwarded: []string{},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Reset state between sub-tests
			s.settingsRepo.Clear()
			s.outputPubSub.ClearMessages()

			tc.setupFilter()

			data := make([]json.RawMessage, len(tc.inputOrgIDs))
			for i, orgID := range tc.inputOrgIDs {
				data[i] = json.RawMessage(validBentoPayload(orgID, "evt_"+orgID))
			}

			batch := RawEventBatch{
				TenantID:      testTenantID,
				EnvironmentID: testEnvironmentID,
				Data:          data,
			}

			err := s.svc.processMessage(buildBatchMsg(batch))

			if tc.wantErr {
				s.Error(err)
				return
			}
			s.NoError(err)

			got := s.publishedExternalIDs()
			wantSorted := append([]string{}, tc.wantForwarded...)
			sort.Strings(wantSorted)

			s.Equal(wantSorted, got,
				"forwarded ExternalCustomerIDs should exactly match allowlist")
		})
	}
}

// ---------------------------------------------------------------------------
// Edge-case tests (kept separate as they need special setup)
// ---------------------------------------------------------------------------

// TestInvalidBentoEvent_SkippedBeforeFilterCheck — invalid events are dropped before the
// filter check; a valid+allowed event in the same batch still gets through.
func (s *RawEventConsumptionSuite) TestInvalidBentoEvent_SkippedBeforeFilterCheck() {
	s.makeFilterSetting(true, []string{"org_001"})

	// Missing required fields (methodName, providerName, id, createdAt)
	invalidPayload := json.RawMessage(`{"orgId":"org_001"}`)

	batch := RawEventBatch{
		TenantID:      testTenantID,
		EnvironmentID: testEnvironmentID,
		Data: []json.RawMessage{
			invalidPayload,
			json.RawMessage(validBentoPayload("org_001", "evt_valid")),
		},
	}

	err := s.svc.processMessage(buildBatchMsg(batch))
	s.NoError(err)
	s.Equal([]string{"org_001"}, s.publishedExternalIDs())
}

// TestMalformedBatchPayload_ReturnsNonRetriableError — a completely non-JSON batch payload
// returns an immediate non-retriable error (no retries would help).
func (s *RawEventConsumptionSuite) TestMalformedBatchPayload_ReturnsNonRetriableError() {
	msg := message.NewMessage("test-uuid", []byte("not-json"))
	err := s.svc.processMessage(msg)
	s.Error(err)
	s.Contains(err.Error(), "non-retriable unmarshal error")
}

// TestSettingsRepoError_FailsBatchForRetry — a transient settings-store error (not ErrNotFound)
// must propagate so Watermill retries the entire batch.
func (s *RawEventConsumptionSuite) TestSettingsRepoError_FailsBatchForRetry() {
	// Replace the settings repo with one that always returns an operational error
	// for GetByKey. We do this by pre-populating a setting with a corrupted value
	// that causes a parse failure (which is treated as a hard error).
	ctx := testutil.SetupContext()
	corruptSetting := &domainSettings.Setting{
		ID:            types.GenerateUUID(),
		Key:           types.SettingKeyEventIngestionFilter,
		Value:         map[string]interface{}{"enabled": "not-a-bool", "allowed_external_customer_ids": "also-wrong"},
		EnvironmentID: testEnvironmentID,
	}
	corruptSetting.TenantID = testTenantID
	corruptSetting.Status = types.StatusPublished
	corruptSetting.CreatedAt = time.Now()
	corruptSetting.UpdatedAt = time.Now()
	require.NoError(s.T(), s.settingsRepo.Create(ctx, corruptSetting))

	batch := RawEventBatch{
		TenantID:      testTenantID,
		EnvironmentID: testEnvironmentID,
		Data:          []json.RawMessage{json.RawMessage(validBentoPayload("org_001", "evt_001"))},
	}

	err := s.svc.processMessage(buildBatchMsg(batch))
	s.Error(err, "corrupt setting value should fail the batch for retry")
	s.Equal(0, len(s.outputPubSub.GetMessages(testOutputTopic)),
		"no events should be forwarded when filter config is unreadable")
}

// TestTenantFallbackFromConfig — when the batch omits tenant/env, config fallback values
// are used to scope the settings lookup and the filter activates correctly.
func (s *RawEventConsumptionSuite) TestTenantFallbackFromConfig() {
	s.GetConfig().Billing.TenantID = testTenantID
	s.GetConfig().Billing.EnvironmentID = testEnvironmentID
	s.makeFilterSetting(true, []string{"org_001"})

	batch := RawEventBatch{
		// TenantID and EnvironmentID intentionally omitted → config fallback
		Data: []json.RawMessage{
			json.RawMessage(validBentoPayload("org_001", "evt_001")), // allowed
			json.RawMessage(validBentoPayload("org_002", "evt_002")), // filtered
		},
	}

	err := s.svc.processMessage(buildBatchMsg(batch))
	s.NoError(err)
	s.Equal([]string{"org_001"}, s.publishedExternalIDs())
}
