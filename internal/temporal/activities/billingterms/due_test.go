package billingterms

import (
	"context"
	"errors"
	"testing"
	"time"

	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	models "github.com/flexprice/flexprice/internal/temporal/models/billingterms"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

type deliveryService struct {
	t      *testing.T
	calls  int
	err    error
	cutoff time.Time
}

func (s *deliveryService) scope(ctx context.Context, cutoff time.Time) {
	s.calls++
	require.Equal(s.t, "tenant", types.GetTenantID(ctx))
	require.Equal(s.t, "env", types.GetEnvironmentID(ctx))
	require.Equal(s.t, "user", types.GetUserID(ctx))
	require.True(s.t, cutoff.Equal(s.cutoff))
	actor, source, ok := types.GetActor(ctx)
	require.True(s.t, ok)
	require.Equal(s.t, types.ActorTypeSystem, actor.Type)
	require.Equal(s.t, types.SourceWorkflow, source)
}
func (s *deliveryService) Prepare(ctx context.Context, request core.DueBatchRequest) (*core.DueBatch, error) {
	s.scope(ctx, request.AsOf)
	return &core.DueBatch{Scanned: 1, Items: []core.DueCandidate{{ItemID: "item"}}}, s.err
}
func (s *deliveryService) CreateOrRecover(ctx context.Context, request core.DueInvoiceRequest) (*core.DueInvoice, error) {
	s.scope(ctx, request.AsOf)
	return &core.DueInvoice{InvoiceID: "inv"}, s.err
}

func TestBillingScheduleActivitiesRestoreScope(t *testing.T) {
	service := &deliveryService{t: t, cutoff: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	activities := NewActivities(service)
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "wrong_tenant"), "wrong_env")
	batch, err := activities.PrepareBillingScheduleBatchActivity(ctx, models.PrepareInput{TenantID: "tenant", EnvironmentID: "env", UserID: "user", Request: core.DueBatchRequest{AsOf: service.cutoff, Limit: 100}})
	require.NoError(t, err)
	require.Len(t, batch.Items, 1)
	invoice, err := activities.CreateOrRecoverBillingScheduleInvoiceActivity(ctx, models.CreateInput{TenantID: "tenant", EnvironmentID: "env", UserID: "user", Request: core.DueInvoiceRequest{ItemID: "item", AsOf: service.cutoff}})
	require.NoError(t, err)
	require.Equal(t, "inv", invoice.InvoiceID)
	require.Equal(t, 2, service.calls)
	_, err = activities.PrepareBillingScheduleBatchActivity(ctx, models.PrepareInput{})
	require.Error(t, err)
	require.Equal(t, 2, service.calls)
}

func TestBillingScheduleActivityErrorClassification(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		stop bool
	}{
		{"validation", ierr.Validation("invalid item"), true},
		{"transient database", errors.New("connection lost"), false},
		{"concurrent change", ierr.NewError("changed").Mark(ierr.ErrVersionConflict), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := activityError(test.err)
			var application *temporal.ApplicationError
			if test.stop {
				require.ErrorAs(t, result, &application)
				require.True(t, application.NonRetryable())
			} else {
				require.ErrorIs(t, result, test.err)
			}
		})
	}
}
