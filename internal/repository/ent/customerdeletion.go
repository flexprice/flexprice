package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/customerdeletioncategory"
	"github.com/flexprice/flexprice/ent/customerdeletionrequest"
	domaincd "github.com/flexprice/flexprice/internal/domain/customerdeletion"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// CustomerDeletionRepository implements the customer deletion repository using ent
type CustomerDeletionRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewCustomerDeletionRepository creates a new customer deletion repository
func NewCustomerDeletionRepository(client postgres.IClient, logger *logger.Logger) *CustomerDeletionRepository {
	return &CustomerDeletionRepository{
		client: client,
		logger: logger,
	}
}

// Create writes the request row and its category rows atomically
func (r *CustomerDeletionRepository) Create(ctx context.Context, request *domaincd.CustomerDeletionRequest, categories []*domaincd.CustomerDeletionCategory) error {
	if request == nil {
		return ierr.NewError("deletion request is required").
			WithHint("A deletion request must be provided").
			Mark(ierr.ErrValidation)
	}

	// SLA clock cannot start at zero
	if request.RequestedAt.IsZero() {
		return ierr.NewError("requested_at is required").
			WithHint("A deletion request must carry its request time").
			Mark(ierr.ErrValidation)
	}

	if err := domaincd.ValidateCategories(request.ID, categories); err != nil {
		return err
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	return r.client.WithTx(ctx, func(txCtx context.Context) error {
		client := r.client.Writer(txCtx)

		create := client.CustomerDeletionRequest.
			Create().
			SetID(request.ID).
			SetTenantID(tenantID).
			SetEnvironmentID(environmentID).
			SetCustomerID(request.CustomerID).
			SetExternalID(request.ExternalID).
			SetState(request.State).
			SetRequestedAt(request.RequestedAt).
			SetRequestedVia(request.RequestedVia).
			SetStatus(string(request.Status)).
			SetCreatedBy(request.CreatedBy).
			SetUpdatedBy(request.UpdatedBy)

		if request.SuppressedAt != nil {
			create = create.SetSuppressedAt(*request.SuppressedAt)
		}
		if request.RedactedAt != nil {
			create = create.SetRedactedAt(*request.RedactedAt)
		}
		if request.CooldownUntil != nil {
			create = create.SetCooldownUntil(*request.CooldownUntil)
		}
		if request.IrreversibleStartedAt != nil {
			create = create.SetIrreversibleStartedAt(*request.IrreversibleStartedAt)
		}
		if request.RetentionPolicy != nil {
			create = create.SetRetentionPolicy(request.RetentionPolicy)
		}
		if request.FailureReason != "" {
			create = create.SetFailureReason(request.FailureReason)
		}

		if _, err := create.Save(txCtx); err != nil {
			if ent.IsConstraintError(err) {
				return ierr.WithError(err).
					WithHint("A deletion request already exists for this customer or external ID").
					WithReportableDetails(map[string]any{
						"customer_id": request.CustomerID,
						"external_id": request.ExternalID,
					}).
					Mark(ierr.ErrAlreadyExists)
			}
			return ierr.WithError(err).
				WithHint("Failed to create customer deletion request").
				Mark(ierr.ErrDatabase)
		}

		for _, category := range categories {
			if category == nil {
				continue
			}

			categoryCreate := client.CustomerDeletionCategory.
				Create().
				SetID(category.ID).
				SetTenantID(tenantID).
				SetEnvironmentID(environmentID).
				SetRequestID(request.ID).
				SetCategory(category.Category).
				SetDisposition(category.Disposition).
				SetStatus(string(category.Status)).
				SetCreatedBy(category.CreatedBy).
				SetUpdatedBy(category.UpdatedBy)

			if category.LegalBasis != "" {
				categoryCreate = categoryCreate.SetLegalBasis(category.LegalBasis)
			}
			if category.VerifiedAt != nil {
				categoryCreate = categoryCreate.SetVerifiedAt(*category.VerifiedAt)
			}
			if category.VerificationResult != nil {
				categoryCreate = categoryCreate.SetVerificationResult(category.VerificationResult)
			}

			if _, err := categoryCreate.Save(txCtx); err != nil {
				if ent.IsConstraintError(err) {
					return ierr.WithError(err).
						WithHint("A category row already exists for this request and category").
						WithReportableDetails(map[string]any{
							"request_id": request.ID,
							"category":   category.Category,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
				return ierr.WithError(err).
					WithHint("Failed to create customer deletion category").
					Mark(ierr.ErrDatabase)
			}
		}

		return nil
	})
}

// GetByCustomerID retrieves the deletion request for an internal customer ID
func (r *CustomerDeletionRepository) GetByCustomerID(ctx context.Context, customerID string) (*domaincd.CustomerDeletionRequest, error) {
	client := r.client.Reader(ctx)
	request, err := client.CustomerDeletionRequest.
		Query().
		Where(
			customerdeletionrequest.CustomerID(customerID),
			customerdeletionrequest.TenantID(types.GetTenantID(ctx)),
			customerdeletionrequest.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.NewError("customer deletion request not found").
				WithHintf("No deletion request exists for customer %s", customerID).
				WithReportableDetails(map[string]any{
					"customer_id": customerID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get customer deletion request").
			Mark(ierr.ErrDatabase)
	}

	return domaincd.FromEnt(request), nil
}

// GetByExternalID retrieves the deletion request for a retired external ID
func (r *CustomerDeletionRepository) GetByExternalID(ctx context.Context, externalID string) (*domaincd.CustomerDeletionRequest, error) {
	client := r.client.Reader(ctx)
	request, err := client.CustomerDeletionRequest.
		Query().
		Where(
			customerdeletionrequest.ExternalID(externalID),
			customerdeletionrequest.TenantID(types.GetTenantID(ctx)),
			customerdeletionrequest.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.NewError("customer deletion request not found").
				WithHintf("No deletion request exists for external ID %s", externalID).
				WithReportableDetails(map[string]any{
					"external_id": externalID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get customer deletion request by external ID").
			Mark(ierr.ErrDatabase)
	}

	return domaincd.FromEnt(request), nil
}

// IsExternalIDRetired reports permanent, never-reusable retirement
func (r *CustomerDeletionRepository) IsExternalIDRetired(ctx context.Context, externalID string) (bool, error) {
	if externalID == "" {
		return false, nil
	}

	// replica lag would release a retired identifier
	retired, err := r.client.Reader(types.WithForceWriter(ctx)).CustomerDeletionRequest.
		Query().
		Where(
			customerdeletionrequest.ExternalID(externalID),
			customerdeletionrequest.TenantID(types.GetTenantID(ctx)),
			customerdeletionrequest.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Exist(ctx)

	if err != nil {
		return false, ierr.WithError(err).
			WithHint("Failed to check the external ID retirement registry").
			WithReportableDetails(map[string]any{
				"external_id": externalID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return retired, nil
}

// ListCategories retrieves the category rows of a request
func (r *CustomerDeletionRepository) ListCategories(ctx context.Context, requestID string) ([]*domaincd.CustomerDeletionCategory, error) {
	client := r.client.Reader(ctx)
	categories, err := client.CustomerDeletionCategory.
		Query().
		Where(
			customerdeletioncategory.RequestID(requestID),
			customerdeletioncategory.TenantID(types.GetTenantID(ctx)),
			customerdeletioncategory.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Order(ent.Asc(customerdeletioncategory.FieldCategory)).
		All(ctx)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list customer deletion categories").
			Mark(ierr.ErrDatabase)
	}

	return domaincd.CategoryListFromEnt(categories), nil
}

// UpdateState transitions state only when the current state equals from
func (r *CustomerDeletionRepository) UpdateState(ctx context.Context, requestID string, from, to string) error {
	client := r.client.Writer(ctx)
	affected, err := client.CustomerDeletionRequest.
		Update().
		Where(
			customerdeletionrequest.ID(requestID),
			customerdeletionrequest.State(from),
			customerdeletionrequest.TenantID(types.GetTenantID(ctx)),
			customerdeletionrequest.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetState(to).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update customer deletion request state").
			Mark(ierr.ErrDatabase)
	}

	if affected == 0 {
		return ierr.NewError("customer deletion request state transition rejected").
			WithHintf("No request %s in state %s for this tenant and environment", requestID, from).
			WithReportableDetails(map[string]any{
				"request_id": requestID,
				"from":       from,
				"to":         to,
			}).
			Mark(ierr.ErrNotFound)
	}

	return nil
}
