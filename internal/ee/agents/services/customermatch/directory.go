package customermatch

import "context"

// CustomerDirectory is the agent's read-only view of the tenant's customers.
// Implementations scope every lookup to the tenant and environment in ctx.
type CustomerDirectory interface {
	SearchByName(ctx context.Context, query string, limit int) ([]CustomerRecord, error)
	SearchByEmail(ctx context.Context, emailOrDomain string, limit int) ([]CustomerRecord, error)
	Get(ctx context.Context, id string) (*CustomerRecord, error)
}

type CustomerMatchServiceInterface interface {
	Match(ctx context.Context, req *MatchRequest, directory CustomerDirectory) (*MatchResult, error)
	IsConfigured() bool
}
