package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/types"
)

type CustomerService interface {
	CreateCustomer(ctx context.Context, req dto.CreateCustomerRequest) (*dto.CustomerResponse, error)
	GetCustomer(ctx context.Context, id string) (*dto.CustomerResponse, error)
	GetCustomers(ctx context.Context, filter types.Filter) (*dto.ListCustomersResponse, error)
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
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	customer := req.ToCustomer(ctx)

	if err := s.repo.Create(ctx, customer); err != nil {
		return nil, fmt.Errorf("failed to create customer: %w", err)
	}

	return &dto.CustomerResponse{Customer: customer}, nil
}

func (s *customerService) GetCustomer(ctx context.Context, id string) (*dto.CustomerResponse, error) {
	customer, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get customer: %w", err)
	}

	return &dto.CustomerResponse{Customer: customer}, nil
}

func (s *customerService) GetCustomers(ctx context.Context, filter types.Filter) (*dto.ListCustomersResponse, error) {
	customers, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get customers: %w", err)
	}

	response := &dto.ListCustomersResponse{
		Customers: make([]dto.CustomerResponse, len(customers)),
	}

	for i, c := range customers {
		response.Customers[i] = dto.CustomerResponse{Customer: c}
	}

	response.Total = len(customers)
	response.Offset = filter.Offset
	response.Limit = filter.Limit

	return response, nil
}

func (s *customerService) UpdateCustomer(ctx context.Context, id string, req dto.UpdateCustomerRequest) (*dto.CustomerResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	customer, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get customer: %w", err)
	}

	if req.Name != nil {
		customer.Name = *req.Name
	}
	if req.ExternalID != nil {
		customer.ExternalID = *req.ExternalID
	}
	if req.Email != nil {
		customer.Email = *req.Email
	}
	customer.UpdatedAt = time.Now().UTC()
	customer.UpdatedBy = types.GetUserID(ctx)

	if err := s.repo.Update(ctx, customer); err != nil {
		return nil, fmt.Errorf("failed to update customer: %w", err)
	}

	return &dto.CustomerResponse{Customer: customer}, nil
}

func (s *customerService) DeleteCustomer(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete customer: %w", err)
	}
	return nil
}
