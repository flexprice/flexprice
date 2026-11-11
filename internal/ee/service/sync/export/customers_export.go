package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

var customersCSVHeaders = []string{
	"customer_id", "external_id", "name", "email", "status",
	"billing_currency", "billing_entity_id",
	"address_line1", "address_line2", "city", "state", "postal_code", "country",
	"timezone", "created_at", "updated_at",
}

const customersMetadataPrefix = "metadata_"

// CustomersExporter exports the saved customer filter as a snapshot.
type CustomersExporter struct {
	customerRepo customer.Repository
}

func NewCustomersExporter(customerRepo customer.Repository) *CustomersExporter {
	return &CustomersExporter{customerRepo: customerRepo}
}

func (e *CustomersExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeCustomers)
}

// PrepareData ignores the time window; it is a snapshot.
func (e *CustomersExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	ctx = types.SetTenantID(ctx, request.TenantID)
	ctx = types.SetEnvironmentID(ctx, request.EnvID)

	filter := types.NewNoLimitCustomerFilter()
	if stored := request.SourceConfig.GetCustomerFilter(); stored != nil {
		copied := *stored
		qf := types.NewNoLimitQueryFilter()
		if stored.QueryFilter != nil {
			qf.Status = stored.QueryFilter.Status
		}
		copied.QueryFilter = qf
		filter = &copied
	}

	customers, err := e.customerRepo.ListAll(ctx, filter)
	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to list customers").
			Mark(ierr.ErrDatabase)
	}

	keys := map[string]struct{}{}
	for _, c := range customers {
		for k := range c.Metadata {
			keys[k] = struct{}{}
		}
	}
	metaKeys := lo.Keys(keys)
	sort.Strings(metaKeys)

	header := append([]string{}, customersCSVHeaders...)
	for _, k := range metaKeys {
		header = append(header, customersMetadataPrefix+k)
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(header); err != nil {
		return nil, 0, ierr.WithError(err).WithHint("Failed to write CSV header").Mark(ierr.ErrInternal)
	}
	for _, c := range customers {
		row := []string{
			c.ID, c.ExternalID, c.Name, c.Email, string(c.Status),
			lo.FromPtr(c.BillingCurrency), lo.FromPtr(c.BillingEntityID),
			c.AddressLine1, c.AddressLine2, c.AddressCity, c.AddressState, c.AddressPostalCode, c.AddressCountry,
			c.Timezone, c.CreatedAt.UTC().Format(time.RFC3339), c.UpdatedAt.UTC().Format(time.RFC3339),
		}
		for _, k := range metaKeys {
			row = append(row, c.Metadata[k])
		}
		if err := w.Write(row); err != nil {
			return nil, 0, ierr.WithError(err).WithHint("Failed to write CSV row").Mark(ierr.ErrInternal)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, 0, ierr.WithError(err).WithHint("Failed to flush CSV writer").Mark(ierr.ErrInternal)
	}
	return buf.Bytes(), len(customers), nil
}
