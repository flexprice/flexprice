package handler

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// fakeEntitlementService is a minimal stub of service.EntitlementService: only GetEntitlement
// is exercised by the filter; the rest panic to catch unexpected use.
type fakeEntitlementService struct {
	service.EntitlementService
	resp *dto.EntitlementResponse
	err  error
}

func (f *fakeEntitlementService) GetEntitlement(_ context.Context, _ string) (*dto.EntitlementResponse, error) {
	return f.resp, f.err
}

func subScopedEntitlement(subID string) *dto.EntitlementResponse {
	return &dto.EntitlementResponse{
		Entitlement: &entitlement.Entitlement{
			ID:         "ent_1",
			EntityType: types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
			EntityID:   subID,
		},
	}
}

func planScopedEntitlement() *dto.EntitlementResponse {
	return &dto.EntitlementResponse{
		Entitlement: &entitlement.Entitlement{
			ID:         "ent_1",
			EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN,
			EntityID:   "plan_1",
		},
	}
}

func entitlementDeriver(t *testing.T, resp *dto.EntitlementResponse, err error) EventDeriver {
	t.Helper()
	return NewEventDeriver(testLogger(t),
		NewEntitlementFilter(&fakeEntitlementService{resp: resp, err: err}, testLogger(t)),
	)
}

func TestDerive_SubscriptionScopedEntitlement_YieldsSubscriptionUpdated(t *testing.T) {
	t.Parallel()

	for _, name := range []types.WebhookEventName{
		types.WebhookEventEntitlementCreated,
		types.WebhookEventEntitlementUpdated,
		types.WebhookEventEntitlementDeleted,
	} {
		d := entitlementDeriver(t, subScopedEntitlement("sub_123"), nil)

		derived := d.Derive(context.Background(), &types.WebhookEvent{
			EventName:     name,
			EntityID:      "ent_1",
			TenantID:      "ten_1",
			EnvironmentID: "env_1",
			UserID:        "usr_1",
		})

		require.Len(t, derived, 1, "event %s should derive one event", name)
		require.Equal(t, types.WebhookEventSubscriptionUpdated, derived[0].EventName)
		require.Equal(t, "sub_123", derived[0].EntityID)
		require.Equal(t, types.SystemEntityTypeSubscription, derived[0].EntityType)
		require.Equal(t, "ten_1", derived[0].TenantID)
		require.Equal(t, "env_1", derived[0].EnvironmentID)
		require.Equal(t, 1, derived[0].DerivationDepth, "derived event should be stamped depth 1")
		require.NotEmpty(t, derived[0].ID)
	}
}

func TestDerive_PlanScopedEntitlement_YieldsNothing(t *testing.T) {
	t.Parallel()

	d := entitlementDeriver(t, planScopedEntitlement(), nil)

	derived := d.Derive(context.Background(), &types.WebhookEvent{
		EventName: types.WebhookEventEntitlementCreated,
		EntityID:  "ent_1",
	})

	require.Empty(t, derived)
}

func TestDerive_NonEntitlementEvent_YieldsNothing(t *testing.T) {
	t.Parallel()

	d := entitlementDeriver(t, subScopedEntitlement("sub_123"), nil)

	derived := d.Derive(context.Background(), &types.WebhookEvent{
		EventName: types.WebhookEventSubscriptionCreated,
		EntityID:  "sub_123",
	})

	require.Empty(t, derived)
}

func TestDerive_ResolutionError_YieldsNothing(t *testing.T) {
	t.Parallel()

	d := entitlementDeriver(t, nil, ierr.NewError("not found").Mark(ierr.ErrNotFound))

	derived := d.Derive(context.Background(), &types.WebhookEvent{
		EventName: types.WebhookEventEntitlementDeleted,
		EntityID:  "ent_gone",
	})

	require.Empty(t, derived)
}

// TestDerive_DepthBackstop verifies the runtime loop backstop: once an event has reached the
// max derivation depth, no further events are derived from it.
func TestDerive_DepthBackstop(t *testing.T) {
	t.Parallel()

	d := entitlementDeriver(t, subScopedEntitlement("sub_123"), nil)

	derived := d.Derive(context.Background(), &types.WebhookEvent{
		EventName:       types.WebhookEventEntitlementCreated,
		EntityID:        "ent_1",
		DerivationDepth: maxDerivationDepth,
	})

	require.Empty(t, derived)
}

// stubFilter is a test double used to exercise the deriver's registration, target-validation,
// and cycle-detection logic without touching real services.
type stubFilter struct {
	sources []types.WebhookEventName
	targets []types.WebhookEventName
	emit    []*types.WebhookEvent
}

func (f stubFilter) SourceEvents() []types.WebhookEventName { return f.sources }
func (f stubFilter) TargetEvents() []types.WebhookEventName { return f.targets }
func (f stubFilter) Derive(_ context.Context, _ *types.WebhookEvent) []*types.WebhookEvent {
	return f.emit
}

// TestDerive_UndeclaredTargetDropped verifies a filter cannot emit an event it did not declare.
func TestDerive_UndeclaredTargetDropped(t *testing.T) {
	t.Parallel()

	d := NewEventDeriver(testLogger(t), stubFilter{
		sources: []types.WebhookEventName{types.WebhookEventCustomerCreated},
		targets: []types.WebhookEventName{types.WebhookEventSubscriptionUpdated},
		emit: []*types.WebhookEvent{
			{EventName: types.WebhookEventSubscriptionUpdated, EntityID: "sub_ok"},
			{EventName: types.WebhookEventPaymentCreated, EntityID: "pay_undeclared"},
		},
	})

	derived := d.Derive(context.Background(), &types.WebhookEvent{EventName: types.WebhookEventCustomerCreated})

	require.Len(t, derived, 1)
	require.Equal(t, types.WebhookEventSubscriptionUpdated, derived[0].EventName)
}

// TestNewEventDeriver_PanicsOnCycle verifies the static loop gate: registering filters whose
// source→target edges form a cycle panics at construction.
func TestNewEventDeriver_PanicsOnCycle(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		NewEventDeriver(testLogger(t),
			stubFilter{
				sources: []types.WebhookEventName{types.WebhookEventSubscriptionUpdated},
				targets: []types.WebhookEventName{types.WebhookEventEntitlementCreated},
			},
			stubFilter{
				sources: []types.WebhookEventName{types.WebhookEventEntitlementCreated},
				targets: []types.WebhookEventName{types.WebhookEventSubscriptionUpdated},
			},
		)
	})
}

// TestNewEventDeriver_PanicsOnLongCycle verifies the loop gate catches multi-hop cycles
// spanning several filters (A -> B -> C -> D -> A), not just direct two-filter loops.
func TestNewEventDeriver_PanicsOnLongCycle(t *testing.T) {
	t.Parallel()

	// WebhookEventName is a string alias, so synthetic names keep the graph shape obvious.
	const (
		a types.WebhookEventName = "evt.a"
		b types.WebhookEventName = "evt.b"
		c types.WebhookEventName = "evt.c"
		d types.WebhookEventName = "evt.d"
	)

	require.Panics(t, func() {
		NewEventDeriver(testLogger(t),
			stubFilter{sources: []types.WebhookEventName{a}, targets: []types.WebhookEventName{b}},
			stubFilter{sources: []types.WebhookEventName{b}, targets: []types.WebhookEventName{c}},
			stubFilter{sources: []types.WebhookEventName{c}, targets: []types.WebhookEventName{d}},
			stubFilter{sources: []types.WebhookEventName{d}, targets: []types.WebhookEventName{a}},
		)
	})
}

// TestNewEventDeriver_LongChainNoCycleOK verifies a long but acyclic chain (A -> B -> C -> D)
// does not trip the gate.
func TestNewEventDeriver_LongChainNoCycleOK(t *testing.T) {
	t.Parallel()

	const (
		a types.WebhookEventName = "evt.a"
		b types.WebhookEventName = "evt.b"
		c types.WebhookEventName = "evt.c"
		d types.WebhookEventName = "evt.d"
	)

	require.NotPanics(t, func() {
		NewEventDeriver(testLogger(t),
			stubFilter{sources: []types.WebhookEventName{a}, targets: []types.WebhookEventName{b}},
			stubFilter{sources: []types.WebhookEventName{b}, targets: []types.WebhookEventName{c}},
			stubFilter{sources: []types.WebhookEventName{c}, targets: []types.WebhookEventName{d}},
		)
	})
}

// TestNewEventDeriver_NoCycleOK verifies a normal acyclic registration does not panic.
func TestNewEventDeriver_NoCycleOK(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		NewEventDeriver(testLogger(t),
			stubFilter{
				sources: []types.WebhookEventName{types.WebhookEventEntitlementCreated},
				targets: []types.WebhookEventName{types.WebhookEventSubscriptionUpdated},
			},
		)
	})
}
