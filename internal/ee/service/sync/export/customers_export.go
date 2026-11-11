package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"sort"
	"strings"
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

const (
	customersMetadataPrefix = "metadata_"
	customersPageSize       = 1000
)

// CustomersExporter exports the saved customer filter as a snapshot.
type CustomersExporter struct {
	customerRepo customer.Repository
}

// neutralizeFormula quotes cells spreadsheets would run as formulas.
func neutralizeFormula(cell string) string {
	if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
		return "'" + cell
	}
	return cell
}

// NewCustomersExporter builds the customers exporter.
func NewCustomersExporter(customerRepo customer.Repository) *CustomersExporter {
	return &CustomersExporter{customerRepo: customerRepo}
}

func (e *CustomersExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeCustomers)
}

func (e *CustomersExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	var buf bytes.Buffer
	count, err := e.WriteData(ctx, request, &buf)
	if err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), count, nil
}

// WriteData ignores the time window.
func (e *CustomersExporter) WriteData(ctx context.Context, request *dto.ExportRequest, w io.Writer) (int, error) {
	ctx = types.SetTenantID(ctx, request.TenantID)
	ctx = types.SetEnvironmentID(ctx, request.EnvID)
	filter := request.SourceConfig.GetCustomerFilter()

	keys := map[string]struct{}{}
	afterID := ""
	for {
		page, err := e.customerRepo.ListMetadataPageByFilter(ctx, filter, afterID, customersPageSize)
		if err != nil {
			return 0, err
		}
		for _, c := range page {
			for k := range c.Metadata {
				keys[k] = struct{}{}
			}
		}
		if len(page) < customersPageSize {
			break
		}
		afterID = page[len(page)-1].ID
	}
	metaKeys := lo.Keys(keys)
	sort.Strings(metaKeys)

	header := append([]string{}, customersCSVHeaders...)
	for _, k := range metaKeys {
		header = append(header, customersMetadataPrefix+k)
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return 0, ierr.WithError(err).WithHint("Failed to write CSV header").Mark(ierr.ErrInternal)
	}

	total := 0
	afterID = ""
	for {
		page, err := e.customerRepo.ListByFilterPage(ctx, filter, afterID, customersPageSize)
		if err != nil {
			return 0, err
		}
		for _, c := range page {
			row := []string{
				c.ID, c.ExternalID, c.Name, c.Email, string(c.Status),
				lo.FromPtr(c.BillingCurrency), lo.FromPtr(c.BillingEntityID),
				c.AddressLine1, c.AddressLine2, c.AddressCity, c.AddressState, c.AddressPostalCode, c.AddressCountry,
				c.Timezone, c.CreatedAt.UTC().Format(time.RFC3339), c.UpdatedAt.UTC().Format(time.RFC3339),
			}
			for _, k := range metaKeys {
				row = append(row, c.Metadata[k])
			}
			for i := range row {
				row[i] = neutralizeFormula(row[i])
			}
			if err := cw.Write(row); err != nil {
				return 0, ierr.WithError(err).WithHint("Failed to write CSV row").Mark(ierr.ErrInternal)
			}
		}
		total += len(page)
		if len(page) < customersPageSize {
			break
		}
		afterID = page[len(page)-1].ID
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return 0, ierr.WithError(err).WithHint("Failed to flush CSV writer").Mark(ierr.ErrInternal)
	}
	return total, nil
}
