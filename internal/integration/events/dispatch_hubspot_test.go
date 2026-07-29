package events

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/zap"
)

func testLoggerEvents() *logger.Logger {
	return &logger.Logger{SugaredLogger: zap.NewNop().Sugar()}
}

// dealOutboundConnection returns a published HubSpot connection with deal outbound sync
// enabled, matching the shape DispatchHubSpotDealLineItemSync requires to proceed past the
// connection lookup.
func dealOutboundConnection(id, environmentID string) *connection.Connection {
	return &connection.Connection{
		ID:            id,
		Name:          "hubspot",
		ProviderType:  types.SecretProviderHubSpot,
		EnvironmentID: environmentID,
		SyncConfig: &types.SyncConfig{
			Deal: &types.EntitySyncConfig{Outbound: true},
		},
		BaseModel: types.BaseModel{
			TenantID:  "ten_1",
			Status:    types.StatusPublished,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
		},
	}
}

func TestDispatchHubSpotDealLineItemSync_NoConnection_SkipsSilently(t *testing.T) {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "ten_1")
	ctx = types.SetEnvironmentID(ctx, "env_1")

	connRepo := testutil.NewInMemoryConnectionStore()
	custRepo := testutil.NewInMemoryCustomerStore()
	eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()

	ev := &types.WebhookEvent{
		EventName:     types.WebhookEventSubscriptionLineItemCreated,
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		Payload:       json.RawMessage(`{"subscription_id":"sub_1","line_item_id":"li_1","customer_id":"cus_1","price_type":"FIXED"}`),
	}

	err := DispatchHubSpotDealLineItemSync(ctx, &config.Configuration{IntegrationEvents: config.IntegrationEventsConfig{Enabled: true}},
		connRepo, custRepo, eimRepo, testLoggerEvents(), ev, "msg-1", types.WebhookEventSubscriptionLineItemCreated)
	if err != nil {
		t.Fatalf("expected nil error when no HubSpot connection exists, got %v", err)
	}
}

func TestDispatchHubSpotDealLineItemSync_DealOutboundDisabled_SkipsSilently(t *testing.T) {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "ten_1")
	ctx = types.SetEnvironmentID(ctx, "env_1")

	connRepo := testutil.NewInMemoryConnectionStore()
	custRepo := testutil.NewInMemoryCustomerStore()
	eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()

	// Connection exists but deal outbound sync is not configured.
	conn := dealOutboundConnection("conn_hubspot", "env_1")
	conn.SyncConfig.Deal.Outbound = false
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}

	ev := &types.WebhookEvent{
		EventName:     types.WebhookEventSubscriptionLineItemCreated,
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		Payload:       json.RawMessage(`{"subscription_id":"sub_1","line_item_id":"li_1","customer_id":"cus_1","price_type":"FIXED"}`),
	}

	err := DispatchHubSpotDealLineItemSync(ctx, &config.Configuration{IntegrationEvents: config.IntegrationEventsConfig{Enabled: true}},
		connRepo, custRepo, eimRepo, testLoggerEvents(), ev, "msg-1", types.WebhookEventSubscriptionLineItemCreated)
	if err != nil {
		t.Fatalf("expected nil error when deal outbound sync is disabled, got %v", err)
	}
}

func TestDispatchHubSpotDealLineItemSync_InvalidPayload_SkipsSilently(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"malformed json", `{"subscription_id":`},
		{"missing subscription_id", `{"line_item_id":"li_1","customer_id":"cus_1","price_type":"FIXED"}`},
		{"missing line_item_id", `{"subscription_id":"sub_1","customer_id":"cus_1","price_type":"FIXED"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = types.SetTenantID(ctx, "ten_1")
			ctx = types.SetEnvironmentID(ctx, "env_1")

			connRepo := testutil.NewInMemoryConnectionStore()
			custRepo := testutil.NewInMemoryCustomerStore()
			eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()

			ev := &types.WebhookEvent{
				EventName:     types.WebhookEventSubscriptionLineItemCreated,
				TenantID:      "ten_1",
				EnvironmentID: "env_1",
				Payload:       json.RawMessage(tt.payload),
			}

			err := DispatchHubSpotDealLineItemSync(ctx, &config.Configuration{IntegrationEvents: config.IntegrationEventsConfig{Enabled: true}},
				connRepo, custRepo, eimRepo, testLoggerEvents(), ev, "msg-1", types.WebhookEventSubscriptionLineItemCreated)
			if err != nil {
				t.Fatalf("expected nil error for invalid payload (%s), got %v", tt.name, err)
			}
		})
	}
}

func TestDispatchHubSpotDealLineItemSync_NonFixedPriceType_SkipsSilently(t *testing.T) {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "ten_1")
	ctx = types.SetEnvironmentID(ctx, "env_1")

	connRepo := testutil.NewInMemoryConnectionStore()
	custRepo := testutil.NewInMemoryCustomerStore()
	eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()

	// Deliberately do NOT seed a connection — a non-FIXED price type must short-circuit
	// before the connection lookup is even attempted.
	ev := &types.WebhookEvent{
		EventName:     types.WebhookEventSubscriptionLineItemCreated,
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		Payload:       json.RawMessage(`{"subscription_id":"sub_1","line_item_id":"li_1","customer_id":"cus_1","price_type":"USAGE"}`),
	}

	err := DispatchHubSpotDealLineItemSync(ctx, &config.Configuration{IntegrationEvents: config.IntegrationEventsConfig{Enabled: true}},
		connRepo, custRepo, eimRepo, testLoggerEvents(), ev, "msg-1", types.WebhookEventSubscriptionLineItemCreated)
	if err != nil {
		t.Fatalf("expected nil error for non-FIXED price type, got %v", err)
	}
}

// TestDispatchHubSpotDealLineItemSync_ConfiguredButTemporalUnavailable_ReturnsError covers the
// closest thing to a "happy path" reachable from a pure unit test. Deeper down,
// DispatchHubSpotDealLineItemSync checks temporalservice.GetGlobalTemporalService() immediately
// after the connection lookup — before the operation/already-synced/deal-ID-resolution logic
// even runs (this mirrors DispatchInvoiceVendorSync/DispatchCustomerVendorSync/
// DispatchSubscriptionVendorSync, which all fail fast the same way). The package-level Temporal
// service is a process-wide singleton (temporalservice.GetGlobalTemporalService) initialized
// exactly once via sync.Once in production bootstrap; there is no exported seam to inject a
// fake TemporalService for a single test without mutating that global for the rest of the test
// binary (which would corrupt TestProcessMessage_CustomerCreatedDispatchError in
// handler_test.go, which depends on it staying nil). So this test asserts the function reaches
// the Temporal dispatch stage (errTemporalUnavailable) given a fully valid, enabled
// configuration, and the deal-ID resolution logic itself (the deepest, most valuable part of
// the happy path) is covered directly against resolveHubSpotDealID below.
func TestDispatchHubSpotDealLineItemSync_ConfiguredButTemporalUnavailable_ReturnsError(t *testing.T) {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "ten_1")
	ctx = types.SetEnvironmentID(ctx, "env_1")

	connRepo := testutil.NewInMemoryConnectionStore()
	custRepo := testutil.NewInMemoryCustomerStore()
	eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()

	if err := connRepo.Create(ctx, dealOutboundConnection("conn_hubspot", "env_1")); err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}

	ev := &types.WebhookEvent{
		EventName:     types.WebhookEventSubscriptionLineItemCreated,
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		Payload:       json.RawMessage(`{"subscription_id":"sub_1","line_item_id":"li_1","customer_id":"cus_1","price_type":"FIXED"}`),
	}

	err := DispatchHubSpotDealLineItemSync(ctx, &config.Configuration{IntegrationEvents: config.IntegrationEventsConfig{Enabled: true}},
		connRepo, custRepo, eimRepo, testLoggerEvents(), ev, "msg-1", types.WebhookEventSubscriptionLineItemCreated)
	if err != errTemporalUnavailable {
		t.Fatalf("expected errTemporalUnavailable once connection+payload are valid, got %v", err)
	}
}

// TestResolveHubSpotDealID directly exercises the unexported deal-ID resolution helper (in the
// same package), since the Temporal-availability short-circuit in
// DispatchHubSpotDealLineItemSync (see above) makes this logic unreachable from the exported
// entry point in a unit-test environment. This is the deepest, most valuable part of the new
// dispatch logic — resolving via an existing mapping, falling back to customer metadata, and
// backfilling the mapping.
func TestResolveHubSpotDealID(t *testing.T) {
	t.Run("existing published mapping resolves without touching customer", func(t *testing.T) {
		ctx := context.Background()
		ctx = types.SetTenantID(ctx, "ten_1")
		ctx = types.SetEnvironmentID(ctx, "env_1")

		eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()
		custRepo := testutil.NewInMemoryCustomerStore() // no customer seeded on purpose

		if err := eimRepo.Create(ctx, &entityintegrationmapping.EntityIntegrationMapping{
			ID:               "eim_1",
			EntityID:         "sub_1",
			EntityType:       types.IntegrationEntityTypeSubscription,
			ProviderType:     string(types.SecretProviderHubSpot),
			ProviderEntityID: "deal_existing",
			EnvironmentID:    "env_1",
			BaseModel:        types.BaseModel{TenantID: "ten_1", Status: types.StatusPublished},
		}); err != nil {
			t.Fatalf("failed to seed mapping: %v", err)
		}

		dealID, err := resolveHubSpotDealID(ctx, eimRepo, custRepo, testLoggerEvents(), "sub_1", "cus_1")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if dealID != "deal_existing" {
			t.Fatalf("expected existing mapping's deal ID, got %q", dealID)
		}
	})

	t.Run("falls back to customer metadata and backfills mapping", func(t *testing.T) {
		ctx := context.Background()
		ctx = types.SetTenantID(ctx, "ten_1")
		ctx = types.SetEnvironmentID(ctx, "env_1")

		eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()
		custRepo := testutil.NewInMemoryCustomerStore()

		if err := custRepo.Create(ctx, &customer.Customer{
			ID:            "cus_1",
			ExternalID:    "ext_cus_1",
			EnvironmentID: "env_1",
			Metadata:      map[string]string{"hubspot_deal_id": "deal_from_metadata"},
			BaseModel:     types.BaseModel{TenantID: "ten_1", Status: types.StatusPublished},
		}); err != nil {
			t.Fatalf("failed to seed customer: %v", err)
		}

		dealID, err := resolveHubSpotDealID(ctx, eimRepo, custRepo, testLoggerEvents(), "sub_1", "cus_1")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if dealID != "deal_from_metadata" {
			t.Fatalf("expected deal ID from customer metadata, got %q", dealID)
		}

		// The resolution should have backfilled a mapping so subsequent lookups are cheap.
		filter := types.NewNoLimitEntityIntegrationMappingFilter()
		filter.EntityID = "sub_1"
		filter.EntityType = types.IntegrationEntityTypeSubscription
		filter.ProviderTypes = []string{string(types.SecretProviderHubSpot)}
		mappings, err := eimRepo.List(ctx, filter)
		if err != nil {
			t.Fatalf("failed to list mappings: %v", err)
		}
		if len(mappings) != 1 || mappings[0].ProviderEntityID != "deal_from_metadata" {
			t.Fatalf("expected a backfilled mapping to deal_from_metadata, got %+v", mappings)
		}
	})

	t.Run("no mapping and no customer metadata resolves empty", func(t *testing.T) {
		ctx := context.Background()
		ctx = types.SetTenantID(ctx, "ten_1")
		ctx = types.SetEnvironmentID(ctx, "env_1")

		eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()
		custRepo := testutil.NewInMemoryCustomerStore()

		if err := custRepo.Create(ctx, &customer.Customer{
			ID:            "cus_1",
			ExternalID:    "ext_cus_1",
			EnvironmentID: "env_1",
			BaseModel:     types.BaseModel{TenantID: "ten_1", Status: types.StatusPublished},
		}); err != nil {
			t.Fatalf("failed to seed customer: %v", err)
		}

		dealID, err := resolveHubSpotDealID(ctx, eimRepo, custRepo, testLoggerEvents(), "sub_1", "cus_1")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if dealID != "" {
			t.Fatalf("expected empty deal ID, got %q", dealID)
		}
	})

	t.Run("customer lookup failure propagates as error", func(t *testing.T) {
		ctx := context.Background()
		ctx = types.SetTenantID(ctx, "ten_1")
		ctx = types.SetEnvironmentID(ctx, "env_1")

		eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()
		custRepo := testutil.NewInMemoryCustomerStore() // customer "cus_missing" not seeded

		_, err := resolveHubSpotDealID(ctx, eimRepo, custRepo, testLoggerEvents(), "sub_1", "cus_missing")
		if err == nil {
			t.Fatal("expected an error when the customer cannot be found")
		}
	})
}

// TestLineItemAlreadySynced directly exercises the unexported idempotency-check helper for the
// same reason as TestResolveHubSpotDealID above: it runs after the Temporal-availability
// short-circuit in DispatchHubSpotDealLineItemSync, so it isn't reachable from the exported
// entry point in a unit-test environment.
func TestLineItemAlreadySynced(t *testing.T) {
	t.Run("no mapping returns false", func(t *testing.T) {
		ctx := context.Background()
		ctx = types.SetTenantID(ctx, "ten_1")
		ctx = types.SetEnvironmentID(ctx, "env_1")

		eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()
		if lineItemAlreadySynced(ctx, eimRepo, "li_1") {
			t.Fatal("expected false when no mapping exists")
		}
	})

	t.Run("existing mapping returns true", func(t *testing.T) {
		ctx := context.Background()
		ctx = types.SetTenantID(ctx, "ten_1")
		ctx = types.SetEnvironmentID(ctx, "env_1")

		eimRepo := testutil.NewInMemoryEntityIntegrationMappingStore()
		if err := eimRepo.Create(ctx, &entityintegrationmapping.EntityIntegrationMapping{
			ID:               "eim_1",
			EntityID:         "li_1",
			EntityType:       types.IntegrationEntityTypeSubscriptionLineItem,
			ProviderType:     string(types.SecretProviderHubSpot),
			ProviderEntityID: "hs_line_item_1",
			EnvironmentID:    "env_1",
			BaseModel:        types.BaseModel{TenantID: "ten_1", Status: types.StatusPublished},
		}); err != nil {
			t.Fatalf("failed to seed mapping: %v", err)
		}

		if !lineItemAlreadySynced(ctx, eimRepo, "li_1") {
			t.Fatal("expected true when a mapping already exists for this line item")
		}
	})
}
