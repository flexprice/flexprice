package repository

import (
	"context"

	"github.com/flexprice/flexprice/ent"
)

type Store struct {
	client *ent.Client
}

func (s *Store) archiveCustomers(ctx context.Context) error {
	_, err := s.client.UpdateCustomers().Save(ctx) // want `CL004: Customer.Update\(\).Save drops the rows the activity log needs; use SaveReturning\(ctx\)`
	if err != nil {
		return err
	}
	if err := s.client.UpdateCustomers().Exec(ctx); err != nil { // want `CL004: Customer.Update\(\).Exec drops the rows`
		return err
	}
	_, err = s.client.DeleteCustomers().Exec(ctx) // want `CL004: Customer.Delete\(\).Exec drops the rows the activity log needs; use ExecReturning\(ctx\)`
	return err
}

func (s *Store) fine(ctx context.Context) error {
	if _, err := s.client.UpdateCustomers().SaveReturning(ctx); err != nil {
		return err
	}
	if _, err := s.client.UpdateCustomer().Save(ctx); err != nil {
		return err
	}
	_, err := s.client.UpdateEvents().Save(ctx)
	return err
}
