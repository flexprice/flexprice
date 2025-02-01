package types

import "fmt"

// WalletStatus represents the current state of a wallet
type WalletStatus string

const (
	WalletStatusActive WalletStatus = "active"
	WalletStatusFrozen WalletStatus = "frozen"
	WalletStatusClosed WalletStatus = "closed"
)

type WalletTransactionFilter struct {
	*QueryFilter
	*TimeRangeFilter
	WalletID          *string            `json:"wallet_id,omitempty"`
	Type              *TransactionType   `json:"type,omitempty"`
	TransactionStatus *TransactionStatus `json:"transaction_status,omitempty"`
	ReferenceType     *string            `json:"reference_type,omitempty"`
	ReferenceID       *string            `json:"reference_id,omitempty"`
}

func NewWalletTransactionFilter() *WalletTransactionFilter {
	return &WalletTransactionFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

func NewNoLimitWalletTransactionFilter() *WalletTransactionFilter {
	return &WalletTransactionFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

func (f WalletTransactionFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.Type != nil {
		if err := f.Type.Validate(); err != nil {
			return err
		}
	}

	if f.TransactionStatus != nil {
		if err := f.TransactionStatus.Validate(); err != nil {
			return err
		}
	}

	if f.ReferenceType != nil && f.ReferenceID == nil || f.ReferenceID != nil && *f.ReferenceID == "" {
		return fmt.Errorf("reference_type and reference_id must be provided together")
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *WalletTransactionFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *WalletTransactionFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *WalletTransactionFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *WalletTransactionFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *WalletTransactionFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *WalletTransactionFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *WalletTransactionFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
