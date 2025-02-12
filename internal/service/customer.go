package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type CustomerService interface {
	CreateCustomer(ctx context.Context, req dto.CreateCustomerRequest) (*dto.CustomerResponse, error)
	GetCustomer(ctx context.Context, id string) (*dto.CustomerResponse, error)
	GetCustomers(ctx context.Context, filter *types.CustomerFilter) (*dto.ListCustomersResponse, error)
	UpdateCustomer(ctx context.Context, id string, req dto.UpdateCustomerRequest) (*dto.CustomerResponse, error)
	DeleteCustomer(ctx context.Context, id string) error
}

type customerService struct {
	repo customer.Repository
}

func NewCustomerService(repo customer.Repository) CustomerService {
	return &customerService{repo: repo}
}

func (s *customerService) CreateCustomer(ctx context.Context, req dto.CreateCustomerRequest) (*dto.CustomerResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, errors.Wrap(errors.ErrValidation, errors.ErrCodeValidation, err.Error())
	}

	cust := req.ToCustomer(ctx)

	// Validate address fields
	if err := customer.ValidateAddress(cust); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeValidation, "invalid address")
	}

	if err := s.repo.Create(ctx, cust); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to create customer")
	}

	return &dto.CustomerResponse{Customer: cust}, nil
}

func (s *customerService) GetCustomer(ctx context.Context, id string) (*dto.CustomerResponse, error) {
	customer, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeNotFound, "failed to get customer")
	}

	return &dto.CustomerResponse{Customer: customer}, nil
}

func (s *customerService) GetCustomers(ctx context.Context, filter *types.CustomerFilter) (*dto.ListCustomersResponse, error) {
	if filter == nil {
		filter = &types.CustomerFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	if err := filter.Validate(); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeValidation, "invalid filter")
	}

	customers, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to list customers")
	}

	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to count customers")
	}

	var response []*dto.CustomerResponse
	for _, c := range customers {
		response = append(response, &dto.CustomerResponse{Customer: c})
	}

	return &dto.ListCustomersResponse{
		Items:      response,
		Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (s *customerService) UpdateCustomer(ctx context.Context, id string, req dto.UpdateCustomerRequest) (*dto.CustomerResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeValidation, err.Error())
	}

	cust, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeNotFound, "failed to get customer")
	}

	// Update basic fields
	if req.ExternalID != nil {
		cust.ExternalID = *req.ExternalID
	}
	if req.Name != nil {
		cust.Name = *req.Name
	}
	if req.Email != nil {
		cust.Email = *req.Email
	}

	// Update address fields
	if req.AddressLine1 != nil {
		cust.AddressLine1 = *req.AddressLine1
	}
	if req.AddressLine2 != nil {
		cust.AddressLine2 = *req.AddressLine2
	}
	if req.AddressCity != nil {
		cust.AddressCity = *req.AddressCity
	}
	if req.AddressState != nil {
		cust.AddressState = *req.AddressState
	}
	if req.AddressPostalCode != nil {
		cust.AddressPostalCode = *req.AddressPostalCode
	}
	if req.AddressCountry != nil {
		cust.AddressCountry = *req.AddressCountry
	}

	// Update metadata if provided
	if req.Metadata != nil {
		cust.Metadata = req.Metadata
	}

	// Validate address fields after update
	if err := customer.ValidateAddress(cust); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeValidation, "invalid address")
	}

	cust.UpdatedAt = time.Now().UTC()
	cust.UpdatedBy = types.GetUserID(ctx)

	if err := s.repo.Update(ctx, cust); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to update customer")
	}

	return &dto.CustomerResponse{Customer: cust}, nil
}

func (s *customerService) DeleteCustomer(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.IsNotFound(err) {
			return errors.Wrap(err, errors.ErrCodeNotFound, "customer not found")
		}
		return errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to delete customer")
	}

	return nil
}
