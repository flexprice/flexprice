package service

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// ForEachConnection runs fn once per published connection of the provider, with that connection
// and its owner entity pinned in ctx. Every connection runs; failures come back joined.
// It returns a not-found error when the provider has no connection.
func ForEachConnection(ctx context.Context, repo connection.Repository, provider types.SecretProvider, fn func(context.Context, *connection.Connection) error) error {
	all, err := repo.ListAllPublished(ctx)
	if err != nil {
		return err
	}
	conns := lo.Filter(all, func(c *connection.Connection, _ int) bool { return c.ProviderType == provider })
	if len(conns) == 0 {
		return ierr.NewError("connection not found").
			WithHintf("Connection with provider %s was not found in this environment", provider).
			Mark(ierr.ErrNotFound)
	}

	var errs []error
	for _, c := range conns {
		connCtx := types.SetConnectionID(ctx, c.ID)
		if owner := lo.FromPtr(c.BillingEntityID); owner != "" {
			connCtx = types.SetBillingEntityID(connCtx, owner)
		}
		if err := fn(connCtx, c); err != nil {
			errs = append(errs, ierr.WithError(err).
				WithReportableDetails(map[string]any{"connection_id": c.ID}).
				Error())
		}
	}
	return errors.Join(errs...)
}

// hasPublishedConnection reports whether the environment has any published connection of the provider.
func hasPublishedConnection(ctx context.Context, repo connection.Repository, provider types.SecretProvider) (bool, error) {
	all, err := repo.ListAllPublished(ctx)
	if err != nil {
		return false, err
	}
	return lo.ContainsBy(all, func(c *connection.Connection) bool { return c.ProviderType == provider }), nil
}
