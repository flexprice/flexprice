package types

import (
	"context"

	"github.com/samber/lo"
)

type Role string

func (r Role) String() string { return string(r) }

const (
	RoleSuperAdmin    Role = "super_admin"
	RoleAllReader     Role = "all_reader"
	RoleAllWriter     Role = "all_writer"
	RoleEventIngestor Role = "event_ingestor"
	RoleEventReader   Role = "event_reader"
)

// AllowedRoles returns the roles assignable to this user type. People hold a
// broad access level over the whole tenant, while service accounts hold either
// full access or a single narrow machine scope, so the two sets are disjoint
// apart from super_admin.
func (ut UserType) AllowedRoles() []Role {
	switch ut {
	case UserTypeUser:
		return []Role{RoleSuperAdmin, RoleAllReader, RoleAllWriter}
	case UserTypeServiceAccount:
		return []Role{RoleSuperAdmin, RoleEventIngestor, RoleEventReader}
	default:
		return nil
	}
}

// IsSuperAdminUser reports whether the caller may act administratively over
// other principals. A service account is never administrative, however its
// stored roles read, so a leaked machine key cannot mint or enumerate
// credentials beyond its own. This is the single definition of the rule shared
// by the route guard and the service layer.
func IsSuperAdminUser(ctx context.Context) bool {
	return !IsServiceAccount(ctx) &&
		lo.Contains(GetRoles(ctx), RoleSuperAdmin.String())
}

// RoleFilter filters the roles returned by GET /rbac/roles. Bound via
// ShouldBindQuery, following the same *QueryFilter-style convention as the
// other GET list endpoints (e.g. ConnectionFilter, MeterFilter) — no
// pagination/time-range fields are needed here since role listing isn't
// paginated.
type RoleFilter struct {
	UserType *UserType `json:"user_type,omitempty" form:"user_type"`
}

type Action string

func (a Action) String() string { return string(a) }

const (
	ActionRead  Action = "read"
	ActionWrite Action = "write"
)

type Entity string

func (e Entity) String() string { return string(e) }

const (
	EntityUser            Entity = "user"
	EntityEnvironment     Entity = "environment"
	EntityEvent           Entity = "event"
	EntityMeter           Entity = "meter"
	EntityPrice           Entity = "price"
	EntityCustomer        Entity = "customer"
	EntityPlan            Entity = "plan"
	EntityAddon           Entity = "addon"
	EntityGroup           Entity = "group"
	EntityAlertSettings   Entity = "alert_settings"
	EntitySubscription    Entity = "subscription"
	EntityWallet          Entity = "wallet"
	EntityTenant          Entity = "tenant"
	EntityInvoice         Entity = "invoice"
	EntityFeature         Entity = "feature"
	EntityEntitlement     Entity = "entitlement"
	EntityCreditGrant     Entity = "creditgrant"
	EntityPayment         Entity = "payment"
	EntityIntegration     Entity = "integration"
	EntityTask            Entity = "task"
	EntityTax             Entity = "tax"
	EntitySecret          Entity = "secret"
	EntityConnection      Entity = "connection"
	EntityCostsheet       Entity = "costsheet"
	EntityCreditNote      Entity = "creditnote"
	EntityCoupon          Entity = "coupon"
	EntityAI              Entity = "ai"
	EntityPortal          Entity = "portal"
	EntityWebhook         Entity = "webhook"
	EntityCron            Entity = "cron"
	EntitySetting         Entity = "setting"
	EntityOAuth           Entity = "oauth"
	EntityCheckoutSession Entity = "checkoutsession"
	EntityWorkflow        Entity = "workflow"
)
