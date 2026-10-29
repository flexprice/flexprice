package quickbooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *InvoiceService) scheduledPaymentItem(ctx context.Context) (*ItemResponse, error) {
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || environmentID == "" {
		return nil, ierr.Validation("scheduled payment export requires tenant and environment")
	}
	digest := sha256.Sum256([]byte(tenantID + "\x00" + environmentID))
	name := "Flexprice scheduled payments " + hex.EncodeToString(digest[:8])
	item, err := s.Client.QueryItemByName(ctx, name)
	if err == nil && item != nil && item.ID != "" {
		return item, nil
	}
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	item, err = s.Client.CreateItem(ctx, &ItemCreateRequest{Name: name, Type: string(ItemTypeService), Description: "Scheduled contract payments", Active: true,
		IncomeAccountRef: &AccountRef{Value: quickBooksIncomeAccount(ctx, s.Client, s.Logger)}})
	if err != nil {
		// A concurrent export may have created the same scoped service item.
		if existing, lookupErr := s.Client.QueryItemByName(ctx, name); lookupErr == nil && existing != nil && existing.ID != "" {
			return existing, nil
		}
		return nil, err
	}
	if item == nil || item.ID == "" {
		return nil, ierr.NewError("QuickBooks returned an invalid scheduled payment item").Mark(ierr.ErrSystem)
	}
	return item, nil
}
