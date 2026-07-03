package events

import "github.com/flexprice/flexprice/internal/types"

// ResolveInvoiceSyncTarget returns the single provider an invoice should sync to.
//
//	allowed        — customer.AllowedIntegrationProviders, in priority order (may be empty)
//	enabledInOrder — providers with an enabled outbound connection, in fixed code order
//
// Returns (target, true) or ("", false) when nothing is resolvable.
//
// It is pure and side-effect-free: it performs no I/O and is the single source of
// truth for invoice-sync routing.
func ResolveInvoiceSyncTarget(allowed []string, enabledInOrder []types.SecretProvider) (types.SecretProvider, bool) {
	// Build a set of enabled providers for O(1) membership checks.
	enabledSet := make(map[types.SecretProvider]struct{}, len(enabledInOrder))
	for _, p := range enabledInOrder {
		enabledSet[p] = struct{}{}
	}

	// Non-empty allow-list: return the first allowed entry that is enabled.
	// Unknown/misspelled strings are simply never in the set, so they are ignored.
	if len(allowed) > 0 {
		for _, a := range allowed {
			p := types.SecretProvider(a)
			if _, ok := enabledSet[p]; ok {
				return p, true
			}
		}
		return "", false
	}

	// Empty allow-list: fall back to the first enabled provider by fixed code order.
	if len(enabledInOrder) > 0 {
		return enabledInOrder[0], true
	}

	return "", false
}
