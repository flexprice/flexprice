package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	entbillingterm "github.com/flexprice/flexprice/ent/billingterm"
	entcommitment "github.com/flexprice/flexprice/ent/commitment"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type billingTermRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

// NewBillingTermRepository returns the Ent-backed billing term repository.
func NewBillingTermRepository(client postgres.IClient, log *logger.Logger) billingterm.TermRepository {
	return &billingTermRepository{
		client: client,
		log:    log,
	}
}

func (r *billingTermRepository) scoped(ctx context.Context) *ent.BillingTermQuery {
	return r.client.Reader(ctx).BillingTerm.Query().
		Where(
			entbillingterm.TenantID(types.GetTenantID(ctx)),
			entbillingterm.EnvironmentID(types.GetEnvironmentID(ctx)),
			entbillingterm.Status(string(types.StatusPublished)),
		)
}

func (r *billingTermRepository) CreateMany(ctx context.Context, terms []*billingterm.Term) ([]*billingterm.Term, error) {
	span := StartRepositorySpan(ctx, "billing_term", "create_many", map[string]interface{}{
		"count": len(terms),
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	builders := make([]*ent.BillingTermCreate, len(terms))
	for i, t := range terms {
		environmentID := t.EnvironmentID
		if environmentID == "" {
			environmentID = types.GetEnvironmentID(ctx)
		}

		builders[i] = client.BillingTerm.Create().
			SetID(t.ID).
			SetOwnerType(t.OwnerType).
			SetOwnerID(t.OwnerID).
			SetKind(t.Kind).
			SetValueType(t.ValueType).
			SetNillableValue(t.Value).
			SetNillableBillTrigger(t.BillTrigger).
			SetNillableBillOn(t.BillOn).
			SetNillableStartDate(t.StartDate).
			SetNillableEndDate(t.EndDate).
			SetNillablePeriodCount(t.PeriodCount).
			SetNillableInvoiceCadence(t.InvoiceCadence).
			SetNillableSplit(t.Split).
			SetNillableInstalmentInvoicing(t.InstalmentInvoicing).
			SetNillableInvoiceLabel(t.InvoiceLabel).
			SetMetadata(t.Metadata).
			SetTenantID(t.TenantID).
			SetEnvironmentID(environmentID).
			SetStatus(string(t.Status)).
			SetCreatedAt(t.CreatedAt).
			SetUpdatedAt(t.UpdatedAt).
			SetCreatedBy(t.CreatedBy).
			SetUpdatedBy(t.UpdatedBy)
	}

	rows, err := client.BillingTerm.CreateBulk(builders...).Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create billing terms").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.TermsFromEnt(rows), nil
}

func (r *billingTermRepository) List(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string) ([]*billingterm.Term, error) {
	span := StartRepositorySpan(ctx, "billing_term", "list", map[string]interface{}{
		"owner_type": ownerType,
		"owner_id":   ownerID,
	})
	defer FinishSpan(span)

	rows, err := r.scoped(ctx).
		Where(
			entbillingterm.OwnerTypeEQ(ownerType),
			entbillingterm.OwnerID(ownerID),
		).
		Order(ent.Asc(entbillingterm.FieldID)).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list billing terms").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.TermsFromEnt(rows), nil
}

func (r *billingTermRepository) ListByIDs(ctx context.Context, ids []string) ([]*billingterm.Term, error) {
	span := StartRepositorySpan(ctx, "billing_term", "list_by_ids", map[string]interface{}{
		"count": len(ids),
	})
	defer FinishSpan(span)

	rows, err := r.client.Reader(ctx).BillingTerm.Query().
		Where(
			entbillingterm.TenantID(types.GetTenantID(ctx)),
			entbillingterm.EnvironmentID(types.GetEnvironmentID(ctx)),
			entbillingterm.IDIn(ids...),
		).
		Order(ent.Asc(entbillingterm.FieldID)).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list billing terms").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.TermsFromEnt(rows), nil
}

func (r *billingTermRepository) Archive(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string) ([]*billingterm.Term, error) {
	span := StartRepositorySpan(ctx, "billing_term", "archive", map[string]interface{}{
		"owner_type": ownerType,
		"owner_id":   ownerID,
	})
	defer FinishSpan(span)

	rows, err := r.client.Writer(ctx).BillingTerm.Update().
		Where(
			entbillingterm.TenantID(types.GetTenantID(ctx)),
			entbillingterm.EnvironmentID(types.GetEnvironmentID(ctx)),
			entbillingterm.Status(string(types.StatusPublished)),
			entbillingterm.OwnerTypeEQ(ownerType),
			entbillingterm.OwnerID(ownerID),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		SaveReturning(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to archive billing terms").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.TermsFromEnt(rows), nil
}

// GetLinkedCustomerID returns the customer of the commitment that owns the term.
func (r *billingTermRepository) GetLinkedCustomerID(ctx context.Context, id string) (string, error) {
	return linkedCustomerID(ctx, "billing_term", id, func(ctx context.Context) (string, error) {
		client := r.client.Reader(ctx)
		term, err := client.BillingTerm.Query().
			Where(
				entbillingterm.ID(id),
				entbillingterm.TenantID(types.GetTenantID(ctx)),
				entbillingterm.EnvironmentID(types.GetEnvironmentID(ctx)),
			).
			Select(entbillingterm.FieldOwnerType, entbillingterm.FieldOwnerID).
			Only(ctx)
		if err != nil {
			return "", err
		}

		if term.OwnerType != types.BillingTermOwnerCommitment {
			return "", nil
		}

		return client.Commitment.Query().
			Where(
				entcommitment.ID(term.OwnerID),
				entcommitment.TenantID(types.GetTenantID(ctx)),
				entcommitment.EnvironmentID(types.GetEnvironmentID(ctx)),
			).
			Select(entcommitment.FieldCustomerID).
			String(ctx)
	})
}
