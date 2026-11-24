package contract

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	"github.com/flexprice/flexprice/internal/types"
)

// ContractService keeps each linked subscription's contract in step with the subscription.
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

// SyncForSubscription derives the status of the subscription's contract; nil for a subscription with no contract.
// It is idempotent.
func (s *contractService) SyncForSubscription(ctx context.Context, sub *subscription.Subscription) (*domainContract.Contract, error) {
	existing, err := s.contractRepo.GetBySubscriptionID(ctx, sub.ID)
	if ierr.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	next := deriveContractStatus(sub, existing, clock.Now())
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
		Items: lo.Map(contracts, func(c *domainContract.Contract, _ int) *dto.ContractResponse {
			return dto.NewContractResponse(c).WithoutExtraction()
		}),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
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
