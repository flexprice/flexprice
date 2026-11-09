package service

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	integrationevents "github.com/flexprice/flexprice/internal/integration/events"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type IntegrationSyncService interface {
	SyncEntity(ctx context.Context, req dto.IntegrationSyncRequest) error
}

type integrationSyncService struct {
	ServiceParams
}

func NewIntegrationSyncService(params ServiceParams) IntegrationSyncService {
	return &integrationSyncService{ServiceParams: params}
}

func (s *integrationSyncService) SyncEntity(ctx context.Context, req dto.IntegrationSyncRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	// One connection named: only its provider is synced, not every connected one.
	var only *connection.Connection
	var opts []integrationevents.DispatchOption
	if req.ConnectionID != "" {
		conn, err := s.ConnectionRepo.Get(ctx, req.ConnectionID)
		if err != nil {
			if ierr.IsNotFound(err) {
				return ierr.NewError("connection not found").
					WithHint("The connection to sync with does not exist in this environment").
					Mark(ierr.ErrValidation)
			}
			return err
		}
		if conn.Status != types.StatusPublished {
			return ierr.NewError("connection is not active").
				WithHint("Sync with a published connection").
				Mark(ierr.ErrValidation)
		}
		only = conn
		opts = append(opts, integrationevents.OnlyProvider(conn.ProviderType))
	}

	switch req.EntityType {
	case types.IntegrationEntityTypeInvoice:
		if req.Method == dto.IntegrationSyncMethodPull {
			return s.pullInvoice(ctx, req.EntityID, only)
		}
		return s.syncInvoice(ctx, req.EntityID, req.Resync, opts...)
	case types.IntegrationEntityTypeCustomer:
		return s.syncCustomer(ctx, req.EntityID, opts...)
	default:
		return ierr.NewError("unsupported entity_type").Mark(ierr.ErrValidation)
	}
}

// pullInvoice reads the invoice back from each published connection, or from `only` when set.
func (s *integrationSyncService) pullInvoice(ctx context.Context, invoiceID string, only *connection.Connection) error {
	var connections []*connection.Connection
	if only != nil {
		connections = []*connection.Connection{only}
	} else {
		all, err := s.ConnectionRepo.List(ctx, &types.ConnectionFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
		})
		if err != nil {
			return err
		}
		connections = all
	}

	for _, conn := range connections {
		if conn.Status != types.StatusPublished {
			continue
		}
		integ, err := s.IntegrationFactory.GetIntegrationByProvider(ctx, conn.ProviderType)
		if err != nil {
			continue
		}
		if err := integ.PullAndUpdateInvoice(ctx, invoiceID); err != nil {
			return err
		}
	}

	return nil
}

func (s *integrationSyncService) syncInvoice(ctx context.Context, invoiceID string, resync bool, opts ...integrationevents.DispatchOption) error {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}
	if inv.InvoiceStatus == types.InvoiceStatusSkipped {
		return ierr.NewError("cannot sync a skipped invoice").
			WithHint("Skipped invoices (zero-dollar) cannot be synced to external vendors").
			Mark(ierr.ErrValidation)
	}

	payload, err := json.Marshal(map[string]string{"invoice_id": invoiceID})
	if err != nil {
		return err
	}

	event := &types.WebhookEvent{
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Payload:       payload,
	}
	return integrationevents.DispatchInvoiceVendorSync(
		ctx, s.Config, s.ConnectionRepo, s.EntityIntegrationMappingRepo, s.Logger, event, "", resync, opts...,
	)
}

func (s *integrationSyncService) syncCustomer(ctx context.Context, customerID string, opts ...integrationevents.DispatchOption) error {
	if _, err := s.CustomerRepo.Get(ctx, customerID); err != nil {
		return err
	}

	payload, err := json.Marshal(webhookDto.InternalCustomerEvent{CustomerID: customerID})
	if err != nil {
		return err
	}

	event := &types.WebhookEvent{
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Payload:       payload,
	}
	return integrationevents.DispatchCustomerVendorSync(
		ctx, s.Config, s.ConnectionRepo, s.EntityIntegrationMappingRepo, s.Logger, event, "", opts...,
	)
}
