package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// TriggeringActor names who initiated an operation, so services can apply
// arrangements a tenant may make for itself but an end customer may not take for
// itself. Never client-supplied: set by the entry point that knows the caller.
//
// The zero value is unset and is treated as the tenant, since every internal and
// admin caller predates this field.
type TriggeringActor string

const (
	TriggeringActorTenant      TriggeringActor = "tenant"
	TriggeringActorEndCustomer TriggeringActor = "end_customer"
)

func (a TriggeringActor) String() string { return string(a) }

// IsEndCustomer reports whether a self-serve customer drove this operation.
func (a TriggeringActor) IsEndCustomer() bool { return a == TriggeringActorEndCustomer }

func (a TriggeringActor) Validate() error {
	allowed := []TriggeringActor{TriggeringActorTenant, TriggeringActorEndCustomer}
	if a != "" && !lo.Contains(allowed, a) {
		return ierr.NewError("invalid triggering actor").
			WithHint("Allowed values: tenant, end_customer").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
