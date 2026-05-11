package types

import "strings"

const (
	// System-managed readonly customer metadata keys (prefix _fp_).
	// Set automatically when subscriptions are created or change type.
	// Never set or modified by user-facing APIs.
	MetaKeyHasStandaloneSub         = "_fp_has_standalone_sub"
	MetaKeyHasParentSub             = "_fp_has_parent_sub"
	MetaKeyHasInheritedSub          = "_fp_has_inherited_sub"
	MetaKeyHasGroupedInvoicingSub   = "_fp_has_grouped_invoicing_sub"
	MetaKeyHasDelegatedInvoicingSub = "_fp_has_delegated_invoicing_sub"

	// SystemMetaKeyPrefix is the prefix reserved for all system-managed metadata keys.
	SystemMetaKeyPrefix = "_fp_"
)

// IsSystemMetaKey reports whether key is system-managed and must not be set by users.
func IsSystemMetaKey(key string) bool {
	return strings.HasPrefix(key, SystemMetaKeyPrefix)
}

// SubscriptionTypeToMetaFlag maps a subscription type to its corresponding
// customer metadata flag key. Returns "" for unknown types.
func SubscriptionTypeToMetaFlag(t SubscriptionType) string {
	switch t {
	case SubscriptionTypeStandalone:
		return MetaKeyHasStandaloneSub
	case SubscriptionTypeParent:
		return MetaKeyHasParentSub
	case SubscriptionTypeInherited:
		return MetaKeyHasInheritedSub
	case SubscriptionTypeGroupedInvoicing:
		return MetaKeyHasGroupedInvoicingSub
	case SubscriptionTypeDelegatedInvoicing:
		return MetaKeyHasDelegatedInvoicingSub
	default:
		return ""
	}
}
