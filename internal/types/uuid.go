package types

import (
	"fmt"

	"github.com/oklog/ulid/v2"
)

// GenerateUUID returns a k-sortable unique identifier
func GenerateUUID() string {
	return ulid.Make().String()
}

// GenerateUUIDWithPrefix returns a k-sortable unique identifier
// with a prefix ex inv_0ujsswThIGTUYm2K8FjOOfXtY1K
func GenerateUUIDWithPrefix(prefix string) string {
	if prefix == "" {
		return GenerateUUID()
	}
	return fmt.Sprintf("%s_%s", prefix, GenerateUUID())
}

const (
	// Prefixes for all domains and entities

	UUID_PREFIX_EVENT                       = "event"
	UUID_PREFIX_METER                       = "meter"
	UUID_PREFIX_PLAN                        = "plan"
	UUID_PREFIX_PRICE                       = "price"
	UUID_PREFIX_INVOICE                     = "inv"
	UUID_PREFIX_INVOICE_LINE_ITEM           = "inv_line"
	UUID_PREFIX_SUBSCRIPTION                = "subs"
	UUID_PREFIX_SUBSCRIPTION_LINE_ITEM      = "subs_line"
	UUID_PREFIX_SUBSCRIPTION_PAUSE          = "pause"
	UUID_PREFIX_SUBSCRIPTION_SCHEDULE       = "sched"
	UUID_PREFIX_SUBSCRIPTION_SCHEDULE_PHASE = "phase"
	UUID_PREFIX_CUSTOMER                    = "cust"
	UUID_PREFIX_WALLET                      = "wallet"
	UUID_PREFIX_WALLET_TRANSACTION          = "wtxn"
	UUID_PREFIX_ENVIRONMENT                 = "env"
	UUID_PREFIX_USER                        = "user"
	UUID_PREFIX_TENANT                      = "tenant"
	UUID_PREFIX_FEATURE                     = "feat"
	UUID_PREFIX_ENTITLEMENT                 = "ent"
	UUID_PREFIX_PAYMENT                     = "pay"
	UUID_PREFIX_PAYMENT_ATTEMPT             = "attempt"
	UUID_PREFIX_TASK                        = "task"
	UUID_PREFIX_SECRET                      = "secret"
	UUID_PREFIX_CREDIT_GRANT                = "cg"

	UUID_PREFIX_WEBHOOK_EVENT = "webhook"
)
