package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	entbillinginstalment "github.com/flexprice/flexprice/ent/billinginstalment"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type billingInstalmentRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

// NewBillingInstalmentRepository returns the Ent-backed billing instalment repository.
func NewBillingInstalmentRepository(client postgres.IClient, log *logger.Logger) billingterm.InstalmentRepository {
	return &billingInstalmentRepository{
		client: client,
		log:    log,
	}
}

// scope matches the caller's published instalments in the current environment.
func (r *billingInstalmentRepository) scope(ctx context.Context) []predicate.BillingInstalment {
	return []predicate.BillingInstalment{
		entbillinginstalment.TenantID(types.GetTenantID(ctx)),
		entbillinginstalment.EnvironmentID(types.GetEnvironmentID(ctx)),
		entbillinginstalment.Status(string(types.StatusPublished)),
	}
}

func (r *billingInstalmentRepository) CreateMany(ctx context.Context, instalments []*billingterm.Instalment) ([]*billingterm.Instalment, error) {
	span := StartRepositorySpan(ctx, "billing_instalment", "create_many", map[string]interface{}{
		"count": len(instalments),
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	builders := make([]*ent.BillingInstalmentCreate, len(instalments))
	for i, in := range instalments {
		environmentID := in.EnvironmentID
		if environmentID == "" {
			environmentID = types.GetEnvironmentID(ctx)
		}

		builders[i] = client.BillingInstalment.Create().
			SetID(in.ID).
			SetOwnerType(in.OwnerType).
			SetOwnerID(in.OwnerID).
			SetNillableBillingTermID(in.BillingTermID).
			SetCustomerID(in.CustomerID).
			SetSubscriptionID(in.SubscriptionID).
			SetCurrency(in.Currency).
			SetKind(in.Kind).
			SetBillTrigger(in.BillTrigger).
			SetBillOn(in.BillOn).
			SetNillableTriggeredAt(in.TriggeredAt).
			SetAmount(in.Amount).
			SetDescription(in.Description).
			SetServicePeriodStart(in.ServicePeriodStart).
			SetServicePeriodEnd(in.ServicePeriodEnd).
			SetDelivery(in.Delivery).
			SetInstalmentStatus(in.InstalmentStatus).
			SetNillableInvoiceID(in.InvoiceID).
			SetMetadata(in.Metadata).
			SetTenantID(in.TenantID).
			SetEnvironmentID(environmentID).
			SetStatus(string(in.Status)).
			SetCreatedAt(in.CreatedAt).
			SetUpdatedAt(in.UpdatedAt).
			SetCreatedBy(in.CreatedBy).
			SetUpdatedBy(in.UpdatedBy)
	}

	rows, err := client.BillingInstalment.CreateBulk(builders...).Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create billing instalments").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.InstalmentsFromEnt(rows), nil
}

func (r *billingInstalmentRepository) Get(ctx context.Context, id string) (*billingterm.Instalment, error) {
	span := StartRepositorySpan(ctx, "billing_instalment", "get", map[string]interface{}{
		"instalment_id": id,
	})
	defer FinishSpan(span)

	row, err := r.client.Reader(ctx).BillingInstalment.Query().
		Where(r.scope(ctx)...).
		Where(entbillinginstalment.ID(id)).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Billing instalment %s not found", id).
				Mark(ierr.ErrNotFound)
		}

		return nil, ierr.WithError(err).
			WithHint("Failed to get billing instalment").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.InstalmentFromEnt(row), nil
}

func (r *billingInstalmentRepository) List(ctx context.Context, filter *billingterm.InstalmentFilter) ([]*billingterm.Instalment, error) {
	if filter == nil {
		filter = &billingterm.InstalmentFilter{}
	}

	span := StartRepositorySpan(ctx, "billing_instalment", "list", map[string]interface{}{
		"owner_type": filter.OwnerType,
	})
	defer FinishSpan(span)

	var preds []predicate.BillingInstalment
	if filter.OwnerType != "" {
		preds = append(preds, entbillinginstalment.OwnerTypeEQ(filter.OwnerType))
	}

	if len(filter.OwnerIDs) > 0 {
		preds = append(preds, entbillinginstalment.OwnerIDIn(filter.OwnerIDs...))
	}

	if len(filter.SubscriptionIDs) > 0 {
		preds = append(preds, entbillinginstalment.SubscriptionIDIn(filter.SubscriptionIDs...))
	}

	if len(filter.Statuses) > 0 {
		preds = append(preds, entbillinginstalment.InstalmentStatusIn(filter.Statuses...))
	}

	if len(filter.Deliveries) > 0 {
		preds = append(preds, entbillinginstalment.DeliveryIn(filter.Deliveries...))
	}

	if filter.BillOnOrBefore != nil {
		preds = append(preds, entbillinginstalment.BillOnLTE(*filter.BillOnOrBefore))
	}

	if len(filter.InvoiceIDs) > 0 {
		preds = append(preds, entbillinginstalment.InvoiceIDIn(filter.InvoiceIDs...))
	}

	if len(filter.IDs) > 0 {
		preds = append(preds, entbillinginstalment.IDIn(filter.IDs...))
	}

	rows, err := r.client.Reader(ctx).BillingInstalment.Query().
		Where(r.scope(ctx)...).
		Where(preds...).
		Order(
			ent.Asc(entbillinginstalment.FieldBillOn),
			ent.Asc(entbillinginstalment.FieldID),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list billing instalments").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.InstalmentsFromEnt(rows), nil
}

func (r *billingInstalmentRepository) Update(ctx context.Context, in *billingterm.Instalment) (*billingterm.Instalment, error) {
	span := StartRepositorySpan(ctx, "billing_instalment", "update", map[string]interface{}{
		"instalment_id": in.ID,
	})
	defer FinishSpan(span)

	update := r.client.Writer(ctx).BillingInstalment.UpdateOneID(in.ID).
		Where(r.scope(ctx)...).
		Where(entbillinginstalment.InstalmentStatusEQ(types.InstalmentStatusScheduled)).
		SetAmount(in.Amount).
		SetBillOn(in.BillOn).
		SetInstalmentStatus(in.InstalmentStatus).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx))

	if in.TriggeredAt != nil {
		update = update.SetTriggeredAt(*in.TriggeredAt)
	} else {
		update = update.ClearTriggeredAt()
	}

	row, err := update.Save(ctx)
	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			exists, existsErr := r.client.Writer(ctx).BillingInstalment.Query().
				Where(r.scope(ctx)...).
				Where(entbillinginstalment.ID(in.ID)).
				Exist(ctx)
			if existsErr == nil && exists {
				return nil, ierr.WithError(err).
					WithHintf("Billing instalment %s is no longer scheduled", in.ID).
					Mark(ierr.ErrInvalidOperation)
			}

			return nil, ierr.WithError(err).
				WithHintf("Billing instalment %s not found", in.ID).
				Mark(ierr.ErrNotFound)
		}

		return nil, ierr.WithError(err).
			WithHint("Failed to update billing instalment").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.InstalmentFromEnt(row), nil
}

func (r *billingInstalmentRepository) Cancel(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string, statuses []types.InstalmentStatus) ([]*billingterm.Instalment, error) {
	if len(statuses) == 0 {
		return nil, ierr.Validation("At least one instalment status is required to cancel instalments")
	}

	span := StartRepositorySpan(ctx, "billing_instalment", "cancel", map[string]interface{}{
		"owner_type": ownerType,
		"owner_id":   ownerID,
	})
	defer FinishSpan(span)

	rows, err := r.client.Writer(ctx).BillingInstalment.Update().
		Where(r.scope(ctx)...).
		Where(
			entbillinginstalment.OwnerTypeEQ(ownerType),
			entbillinginstalment.OwnerID(ownerID),
			entbillinginstalment.InstalmentStatusIn(statuses...),
			entbillinginstalment.InstalmentStatusNEQ(types.InstalmentStatusCancelled),
		).
		SetInstalmentStatus(types.InstalmentStatusCancelled).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		SaveReturning(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to cancel billing instalments").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.InstalmentsFromEnt(rows), nil
}

func (r *billingInstalmentRepository) Claim(ctx context.Context, invoiceID string, ids []string) ([]*billingterm.Instalment, error) {
	if len(ids) == 0 {
		return []*billingterm.Instalment{}, nil
	}

	span := StartRepositorySpan(ctx, "billing_instalment", "claim", map[string]interface{}{
		"invoice_id": invoiceID,
		"count":      len(ids),
	})
	defer FinishSpan(span)

	rows, err := r.client.Writer(ctx).BillingInstalment.Update().
		Where(r.scope(ctx)...).
		Where(
			entbillinginstalment.IDIn(ids...),
			entbillinginstalment.InstalmentStatusEQ(types.InstalmentStatusScheduled),
		).
		SetInstalmentStatus(types.InstalmentStatusInvoiced).
		SetInvoiceID(invoiceID).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		SaveReturning(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to claim billing instalments").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.InstalmentsFromEnt(rows), nil
}

func (r *billingInstalmentRepository) Reschedule(ctx context.Context, invoiceID string) ([]*billingterm.Instalment, error) {
	span := StartRepositorySpan(ctx, "billing_instalment", "reschedule", map[string]interface{}{
		"invoice_id": invoiceID,
	})
	defer FinishSpan(span)

	rows, err := r.client.Writer(ctx).BillingInstalment.Update().
		Where(r.scope(ctx)...).
		Where(
			entbillinginstalment.InvoiceID(invoiceID),
			entbillinginstalment.InstalmentStatusEQ(types.InstalmentStatusInvoiced),
		).
		SetInstalmentStatus(types.InstalmentStatusScheduled).
		ClearInvoiceID().
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		SaveReturning(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to reschedule billing instalments").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingterm.InstalmentsFromEnt(rows), nil
}
