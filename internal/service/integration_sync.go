package service

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	integrationevents "github.com/flexprice/flexprice/internal/integration/events"
	"github.com/flexprice/flexprice/internal/integration/paddle"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// IntegrationSyncOutcome holds optional synchronous sync payloads for HTTP responses.
type IntegrationSyncOutcome struct {
	PaddleInvoice *paddle.SyncInvoiceResponse `json:"-"`
}

type IntegrationSyncService interface {
	SyncEntity(ctx context.Context, req dto.IntegrationSyncRequest) (*IntegrationSyncOutcome, error)
}

type integrationSyncService struct {
	ServiceParams
}

func NewIntegrationSyncService(params ServiceParams) IntegrationSyncService {
	return &integrationSyncService{ServiceParams: params}
}

func (s *integrationSyncService) SyncEntity(ctx context.Context, req dto.IntegrationSyncRequest) (*IntegrationSyncOutcome, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if req.ProviderType != "" && types.SecretProvider(req.ProviderType) == types.SecretProviderPaddle {
		return s.syncInvoiceToPaddle(ctx, req.EntityID)
	}

	switch req.EntityType {
	case types.IntegrationEntityTypeInvoice:
		if err := s.syncInvoice(ctx, req.EntityID); err != nil {
			return nil, err
		}
		return &IntegrationSyncOutcome{}, nil
	case types.IntegrationEntityTypeCustomer:
		if err := s.syncCustomer(ctx, req.EntityID); err != nil {
			return nil, err
		}
		return &IntegrationSyncOutcome{}, nil
	default:
		return nil, ierr.NewError("unsupported entity_type").Mark(ierr.ErrValidation)
	}
}

func (s *integrationSyncService) ensureInvoiceVendorSyncable(ctx context.Context, invoiceID string) error {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}
	if inv.InvoiceStatus == types.InvoiceStatusSkipped {
		return ierr.NewError("cannot sync a skipped invoice").
			WithHint("Skipped invoices (zero-dollar) cannot be synced to external vendors").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (s *integrationSyncService) syncInvoiceToPaddle(ctx context.Context, invoiceID string) (*IntegrationSyncOutcome, error) {
	if err := s.ensureInvoiceVendorSyncable(ctx, invoiceID); err != nil {
		return nil, err
	}
	if s.IntegrationFactory == nil {
		return nil, ierr.NewError("integration factory not configured").
			Mark(ierr.ErrInternal)
	}
	paddleIntegration, err := s.IntegrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := paddleIntegration.SyncSvc.SyncInvoice(ctx, paddle.SyncInvoiceRequest{InvoiceID: invoiceID})
	if err != nil {
		return nil, err
	}
	return &IntegrationSyncOutcome{PaddleInvoice: resp}, nil
}

func (s *integrationSyncService) syncInvoice(ctx context.Context, invoiceID string) error {
	if err := s.ensureInvoiceVendorSyncable(ctx, invoiceID); err != nil {
		return err
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
