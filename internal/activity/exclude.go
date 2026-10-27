package activity

import (
	"strings"

	"github.com/samber/lo"
)

// excludingSink drops the batches of excluded tenants before they reach next.
type excludingSink struct {
	next     Sink
	excluded map[string]struct{}
}

// excludeTenants wraps next to drop batches of "tenant_id:environment_id" or "tenant_id" entries; empty passes all.
func excludeTenants(next Sink, entries []string) Sink {
	excluded := lo.Keyify(lo.FilterMap(entries, func(entry string, _ int) (string, bool) {
		entry = strings.TrimSpace(entry)
		if !strings.Contains(entry, ":") {
			entry += ":*"
		}
		return entry, entry != ":*"
	}))
	if len(excluded) == 0 {
		return next
	}
	return excludingSink{next: next, excluded: excluded}
}

func (s excludingSink) Publish(b Batch) {
	_, tenant := s.excluded[b.TenantID+":*"]
	_, pair := s.excluded[b.TenantID+":"+b.EnvironmentID]
	if tenant || pair {
		return
	}
	s.next.Publish(b)
}
