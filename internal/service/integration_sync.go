package service

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/api/dto"
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

	switch req.EntityType {
	case types.IntegrationEntityTypeInvoice:
		if req.Method == dto.IntegrationSyncMethodPull {
			return s.pullInvoice(ctx, req.EntityID)
		}
		return s.syncInvoice(ctx, req.EntityID)
	case types.IntegrationEntityTypeCustomer:
		return s.syncCustomer(ctx, req.EntityID)
	default:
		return ierr.NewError("unsupported entity_type").Mark(ierr.ErrValidation)
	}
}

func (s *integrationSyncService) pullInvoice(ctx context.Context, invoiceID string) error {
	connections, err := s.ConnectionRepo.List(ctx, &types.ConnectionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		return err
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

func (s *integrationSyncService) syncInvoice(ctx context.Context, invoiceID string) error {
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
		ctx, s.Config, s.ConnectionRepo, s.EntityIntegrationMappingRepo, s.Logger, event, "",
	)
}

func (s *integrationSyncService) syncCustomer(ctx context.Context, customerID string) error {
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
		ctx, s.Config, s.ConnectionRepo, s.EntityIntegrationMappingRepo, s.Logger, event, "",
	)
}
