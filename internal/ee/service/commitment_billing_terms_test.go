package service

import domain "github.com/flexprice/flexprice/internal/domain/billingterms"

type billingAPIState struct {
	owner   *domain.OwnerSnapshot
	rules   []*domain.Rule
	items   []*domain.ScheduleItem
	summary domain.CollectionSummary
	noop    bool
}

func (s *billingAPIState) Owner() *domain.OwnerSnapshot       { return s.owner }
func (s *billingAPIState) Rules() []*domain.Rule              { return s.rules }
func (s *billingAPIState) Items() []*domain.ScheduleItem      { return s.items }
func (s *billingAPIState) Summary() *domain.CollectionSummary { copy := s.summary; return &copy }
func (s *billingAPIState) StateToken() string                 { return "fresh_state" }
func (s *billingAPIState) Noop() bool                         { return s.noop }
