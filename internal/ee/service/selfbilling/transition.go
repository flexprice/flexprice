package selfbilling

import "github.com/flexprice/flexprice/internal/types"

type decision struct {
	next      types.TenantInternalStatus
	changed   bool
	provision bool
}

// decide maps the Mumbai subscription onto the local status. Active tenants are never auto-suspended.
func decide(local types.TenantInternalStatus, remote *remoteSubscription) decision {
	keep := decision{next: local}
	if remote == nil {
		keep.provision = local == types.TenantInternalStatusTrialing
		return keep
	}
	switch remote.status {
	case "active":
		if local == types.TenantInternalStatusActive {
			return keep
		}
		return decision{next: types.TenantInternalStatusActive, changed: true}
	case "incomplete", "cancelled":
		if local != types.TenantInternalStatusTrialing {
			return keep
		}
		return decision{next: types.TenantInternalStatusSuspended, changed: true}
	default:
		return keep
	}
}
