package handler

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	cascaderules "github.com/flexprice/flexprice/internal/webhook/handler/cascade_rules"
	"github.com/stretchr/testify/require"
)

// fakeEntitlementService is a minimal stub of service.EntitlementService: only GetEntitlement
// is exercised by the cascade rule; the rest panic to catch unexpected use.
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

func entitlementCascader(t *testing.T, resp *dto.EntitlementResponse, err error) EventCascader {
	t.Helper()
	return NewEventCascader(testLogger(t),
		cascaderules.NewEntitlementCascadeRule(&fakeEntitlementService{resp: resp, err: err}, testLogger(t)),
	)
}

func TestCascade_SubscriptionScopedEntitlement_YieldsSubscriptionUpdated(t *testing.T) {
	t.Parallel()

	for _, name := range []types.WebhookEventName{
		types.WebhookEventEntitlementCreated,
		types.WebhookEventEntitlementUpdated,
		types.WebhookEventEntitlementDeleted,
	} {
		c := entitlementCascader(t, subScopedEntitlement("sub_123"), nil)

		cascaded := c.GetCascadedEvents(context.Background(), &types.WebhookEvent{
			EventName:     name,
			EntityID:      "ent_1",
			TenantID:      "ten_1",
			EnvironmentID: "env_1",
			UserID:        "usr_1",
		})

		require.Len(t, cascaded, 1, "event %s should cascade one event", name)
		require.Equal(t, types.WebhookEventSubscriptionUpdated, cascaded[0].EventName)
		require.Equal(t, "sub_123", cascaded[0].EntityID)
		require.Equal(t, types.SystemEntityTypeSubscription, cascaded[0].EntityType)
		require.Equal(t, "ten_1", cascaded[0].TenantID)
		require.Equal(t, "env_1", cascaded[0].EnvironmentID)
		require.Equal(t, 1, cascaded[0].CascadeDepth, "cascaded event should be stamped depth 1")
		require.NotEmpty(t, cascaded[0].ID)
	}
}

func TestCascade_PlanScopedEntitlement_YieldsNothing(t *testing.T) {
	t.Parallel()

	c := entitlementCascader(t, planScopedEntitlement(), nil)

	cascaded := c.GetCascadedEvents(context.Background(), &types.WebhookEvent{
		EventName: types.WebhookEventEntitlementCreated,
		EntityID:  "ent_1",
	})

	require.Empty(t, cascaded)
}

func TestCascade_NonEntitlementEvent_YieldsNothing(t *testing.T) {
	t.Parallel()

	c := entitlementCascader(t, subScopedEntitlement("sub_123"), nil)

	cascaded := c.GetCascadedEvents(context.Background(), &types.WebhookEvent{
		EventName: types.WebhookEventSubscriptionCreated,
		EntityID:  "sub_123",
	})

	require.Empty(t, cascaded)
}

func TestCascade_ResolutionError_YieldsNothing(t *testing.T) {
	t.Parallel()

	c := entitlementCascader(t, nil, ierr.NewError("not found").Mark(ierr.ErrNotFound))

	cascaded := c.GetCascadedEvents(context.Background(), &types.WebhookEvent{
		EventName: types.WebhookEventEntitlementDeleted,
		EntityID:  "ent_gone",
	})

	require.Empty(t, cascaded)
}

// TestCascade_DepthBackstop verifies the runtime loop backstop: once an event has reached the
// max cascade depth, no further events are cascaded from it.
func TestCascade_DepthBackstop(t *testing.T) {
	t.Parallel()

	c := entitlementCascader(t, subScopedEntitlement("sub_123"), nil)

	cascaded := c.GetCascadedEvents(context.Background(), &types.WebhookEvent{
		EventName:    types.WebhookEventEntitlementCreated,
		EntityID:     "ent_1",
		CascadeDepth: maxCascadeDepth,
	})

	require.Empty(t, cascaded)
}

// stubCascadeRule is a test double used to exercise the cascader's registration, target
// validation, and cycle-detection logic without touching real services.
type stubCascadeRule struct {
	sources []types.WebhookEventName
	targets []types.WebhookEventName
	emit    []*types.WebhookEvent
}

func (r stubCascadeRule) SourceEvents() []types.WebhookEventName { return r.sources }
func (r stubCascadeRule) TargetEvents() []types.WebhookEventName { return r.targets }
func (r stubCascadeRule) Cascade(_ context.Context, _ *types.WebhookEvent) []*types.WebhookEvent {
	return r.emit
}

// TestCascade_UndeclaredTargetDropped verifies a rule cannot emit an event it did not declare.
func TestCascade_UndeclaredTargetDropped(t *testing.T) {
	t.Parallel()

	c := NewEventCascader(testLogger(t), stubCascadeRule{
		sources: []types.WebhookEventName{types.WebhookEventCustomerCreated},
		targets: []types.WebhookEventName{types.WebhookEventSubscriptionUpdated},
		emit: []*types.WebhookEvent{
			{EventName: types.WebhookEventSubscriptionUpdated, EntityID: "sub_ok"},
			{EventName: types.WebhookEventPaymentCreated, EntityID: "pay_undeclared"},
		},
	})

	cascaded := c.GetCascadedEvents(context.Background(), &types.WebhookEvent{EventName: types.WebhookEventCustomerCreated})

	require.Len(t, cascaded, 1)
	require.Equal(t, types.WebhookEventSubscriptionUpdated, cascaded[0].EventName)
}

// TestNewEventCascader_PanicsOnCycle verifies the static loop gate: registering rules whose
// source→target edges form a cycle panics at construction.
func TestNewEventCascader_PanicsOnCycle(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		NewEventCascader(testLogger(t),
			stubCascadeRule{
				sources: []types.WebhookEventName{types.WebhookEventSubscriptionUpdated},
				targets: []types.WebhookEventName{types.WebhookEventEntitlementCreated},
			},
			stubCascadeRule{
				sources: []types.WebhookEventName{types.WebhookEventEntitlementCreated},
				targets: []types.WebhookEventName{types.WebhookEventSubscriptionUpdated},
			},
		)
	})
}

// TestNewEventCascader_PanicsOnLongCycle verifies the loop gate catches multi-hop cycles
// spanning several rules (A -> B -> C -> D -> A), not just direct two-rule loops.
func TestNewEventCascader_PanicsOnLongCycle(t *testing.T) {
	t.Parallel()

	// WebhookEventName is a string alias, so synthetic names keep the graph shape obvious.
	const (
		a types.WebhookEventName = "evt.a"
		b types.WebhookEventName = "evt.b"
		c types.WebhookEventName = "evt.c"
		d types.WebhookEventName = "evt.d"
	)

	require.Panics(t, func() {
		NewEventCascader(testLogger(t),
			stubCascadeRule{sources: []types.WebhookEventName{a}, targets: []types.WebhookEventName{b}},
			stubCascadeRule{sources: []types.WebhookEventName{b}, targets: []types.WebhookEventName{c}},
			stubCascadeRule{sources: []types.WebhookEventName{c}, targets: []types.WebhookEventName{d}},
			stubCascadeRule{sources: []types.WebhookEventName{d}, targets: []types.WebhookEventName{a}},
		)
	})
}

// TestNewEventCascader_LongChainNoCycleOK verifies a long but acyclic chain (A -> B -> C -> D)
// does not trip the gate.
func TestNewEventCascader_LongChainNoCycleOK(t *testing.T) {
	t.Parallel()

	const (
		a types.WebhookEventName = "evt.a"
		b types.WebhookEventName = "evt.b"
		c types.WebhookEventName = "evt.c"
		d types.WebhookEventName = "evt.d"
	)

	require.NotPanics(t, func() {
		NewEventCascader(testLogger(t),
			stubCascadeRule{sources: []types.WebhookEventName{a}, targets: []types.WebhookEventName{b}},
			stubCascadeRule{sources: []types.WebhookEventName{b}, targets: []types.WebhookEventName{c}},
			stubCascadeRule{sources: []types.WebhookEventName{c}, targets: []types.WebhookEventName{d}},
		)
	})
}

// TestNewEventCascader_NoCycleOK verifies a normal acyclic registration does not panic.
func TestNewEventCascader_NoCycleOK(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		NewEventCascader(testLogger(t),
			stubCascadeRule{
				sources: []types.WebhookEventName{types.WebhookEventEntitlementCreated},
				targets: []types.WebhookEventName{types.WebhookEventSubscriptionUpdated},
			},
		)
	})
}
