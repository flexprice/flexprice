package contract

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// ContractService keeps each subscription's contract in step with the subscription.
type ContractService interface {
	SyncForSubscription(ctx context.Context, sub *subscription.Subscription) (*domainContract.Contract, error)
	ArchiveForSubscription(ctx context.Context, subscriptionID string) error
	GetContract(ctx context.Context, id string) (*dto.ContractResponse, error)
	GetContracts(ctx context.Context, filter *contractRepo.Filter) (*dto.ListContractsResponse, error)
}

type contractService struct {
	contractRepo contractRepo.Repository
}

func NewContractService(contractRepo contractRepo.Repository) ContractService {
	return &contractService{contractRepo: contractRepo}
}

// SyncForSubscription creates the subscription's contract if missing and derives its status; it is idempotent.
func (s *contractService) SyncForSubscription(ctx context.Context, sub *subscription.Subscription) (*domainContract.Contract, error) {
	existing, err := s.contractRepo.GetBySubscriptionID(ctx, sub.ID)
	if ierr.IsNotFound(err) {
		existing, err = s.createForSubscription(ctx, sub)
	}
	if err != nil {
		return nil, err
	}

	next := deriveContractStatus(sub, existing, time.Now().UTC())
	if err := s.contractRepo.Update(ctx, next); err != nil {
		return nil, err
	}
	return next, nil
}

// ArchiveForSubscription archives the draft contract of an abandoned draft subscription.
func (s *contractService) ArchiveForSubscription(ctx context.Context, subscriptionID string) error {
	existing, err := s.contractRepo.GetBySubscriptionID(ctx, subscriptionID)
	if ierr.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if existing.ContractStatus() != types.ContractStatusDraft {
		return nil
	}

	return s.contractRepo.Update(ctx, domainContract.NewBuilder(existing).WithStatus(types.StatusArchived).Build())
}

// GetContract returns the contract with its associations.
func (s *contractService) GetContract(ctx context.Context, id string) (*dto.ContractResponse, error) {
	c, err := s.contractRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	associations, err := s.contractRepo.ListAssociations(ctx, id)
	if err != nil {
		return nil, err
	}
	return dto.NewContractResponse(c).WithAssociations(associations), nil
}

func (s *contractService) GetContracts(ctx context.Context, filter *contractRepo.Filter) (*dto.ListContractsResponse, error) {
	if filter == nil {
		filter = contractRepo.NewFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	contracts, err := s.contractRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count := len(contracts)
	if !filter.IsUnlimited() {
		count, err = s.contractRepo.Count(ctx, filter)
		if err != nil {
			return nil, err
		}
	}

	return &dto.ListContractsResponse{
		Items:      lo.Map(contracts, func(c *domainContract.Contract, _ int) *dto.ContractResponse { return dto.NewContractResponse(c) }),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (s *contractService) createForSubscription(ctx context.Context, sub *subscription.Subscription) (*domainContract.Contract, error) {
	now := time.Now().UTC()
	c := domainContract.NewBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONTRACT)).
		WithCustomerID(sub.CustomerID).
		WithCurrency(sub.Currency).
		WithTermEnd(sub.EndDate).
		WithContractStatus(types.ContractStatusDraft).
		WithStatus(types.StatusPublished).
		WithCreatedAt(now).
		WithUpdatedAt(now).
		Build()

	a := domainContract.NewAssociationBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONTRACT_ASSOCIATION)).
		WithContractID(c.ID()).
		WithEntityType(types.RevRecEntityTypeSubscription).
		WithEntityID(sub.ID).
		WithStatus(types.StatusPublished).
		WithCreatedAt(now).
		Build()

	err := s.contractRepo.CreateWithAssociation(ctx, c, a)
	if ierr.IsAlreadyExists(err) {
		return s.contractRepo.GetBySubscriptionID(ctx, sub.ID)
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// deriveContractStatus returns the contract after applying the subscription's current state.
// ended and cancelled are terminal; an active contract stays active through incomplete and pause.
func deriveContractStatus(sub *subscription.Subscription, existing *domainContract.Contract, now time.Time) *domainContract.Contract {
	b := domainContract.NewBuilder(existing)

	switch existing.ContractStatus() {
	case types.ContractStatusDraft:
		b.WithTermEnd(sub.EndDate)
		switch sub.SubscriptionStatus {
		case types.SubscriptionStatusActive, types.SubscriptionStatusPaused:
			b.WithContractStatus(types.ContractStatusActive).WithTermStart(lo.ToPtr(sub.StartDate))
		case types.SubscriptionStatusCancelled:
			// Cancelled after its start, it delivered service and ended, e.g. first synced after reaching its end date.
			if sub.CancelledAt != nil && sub.CancelledAt.After(sub.StartDate) {
				b.WithContractStatus(types.ContractStatusEnded).WithTermStart(lo.ToPtr(sub.StartDate)).WithEndedAt(sub.CancelledAt)
			} else {
				b.WithContractStatus(types.ContractStatusCancelled)
			}
		}

	case types.ContractStatusActive:
		b.WithTermEnd(sub.EndDate)
		if sub.SubscriptionStatus == types.SubscriptionStatusCancelled {
			b.WithContractStatus(types.ContractStatusEnded).WithEndedAt(lo.Ternary(sub.CancelledAt != nil, sub.CancelledAt, &now))
		}
	}

	return b.Build()
}
