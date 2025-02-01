package types

import "fmt"

// TransactionType represents the type of wallet transaction
type TransactionType string

const (
	TransactionTypeCredit TransactionType = "credit"
	TransactionTypeDebit  TransactionType = "debit"
)

func (t TransactionType) String() string {
	return string(t)
}

func (t TransactionType) Validate() error {
	allowedTypes := []TransactionType{TransactionTypeCredit, TransactionTypeDebit}
	for _, allowedType := range allowedTypes {
		if t == allowedType {
			return nil
		}
	}
	return fmt.Errorf("invalid transaction type: %s", t)
}

// TransactionStatus represents the status of a wallet transaction
type TransactionStatus string

const (
	TransactionStatusPending   TransactionStatus = "pending"
	TransactionStatusCompleted TransactionStatus = "completed"
	TransactionStatusFailed    TransactionStatus = "failed"
)

func (t TransactionStatus) String() string {
	return string(t)
}

func (t TransactionStatus) Validate() error {
	allowedStatuses := []TransactionStatus{TransactionStatusPending, TransactionStatusCompleted, TransactionStatusFailed}
	for _, allowedStatus := range allowedStatuses {
		if t == allowedStatus {
			return nil
		}
	}
	return fmt.Errorf("invalid transaction status: %s", t)
}
