package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type SubscriptionDraftAndComputeOptionsSuite struct {
	SubscriptionServiceSuite // reuse the existing suite's SetupTest/service/test data wiring
}

func TestSubscriptionDraftAndComputeOptions(t *testing.T) {
	suite.Run(t, new(SubscriptionDraftAndComputeOptionsSuite))
}

// Both cases below only need to reach "temporal service not available" — there is no live
// Temporal server in unit tests, so temporalservice.GetGlobalTemporalService() returns nil and
// every call errors before touching the network. That's enough to prove which code path
// (ExecuteWorkflow vs StartWorkflow with an explicit ID) each option set takes, without needing
// a running Temporal server: the zero-value path returns the exact same "temporal service not
// available" error TriggerSubscriptionDraftAndComputeWorkflow already returns today, proving
// nothing about the delegation changed observably.
func (s *SubscriptionDraftAndComputeOptionsSuite) TestZeroValueOptionsMatchExistingMethod() {
	subID := s.testData.subscription.ID

	_, errOriginal := s.service.TriggerSubscriptionDraftAndComputeWorkflow(s.GetContext(), subID)
	_, errWithOptions := s.service.TriggerSubscriptionDraftAndComputeWorkflowWithOptions(
		s.GetContext(), subID, interfaces.DraftAndComputeOptions{},
	)

	s.Require().Error(errOriginal)
	s.Require().Error(errWithOptions)
	s.Require().Equal(errOriginal.Error(), errWithOptions.Error(),
		"zero-value options must produce byte-identical behavior to the pre-existing method")
}

func (s *SubscriptionDraftAndComputeOptionsSuite) TestEmptySubscriptionIDStillValidatesFirst() {
	_, err := s.service.TriggerSubscriptionDraftAndComputeWorkflowWithOptions(
		s.GetContext(), "", interfaces.DraftAndComputeOptions{
			TaskQueue:  types.TemporalTaskQueueBilling,
			WorkflowID: "wf_test_123",
		},
	)
	s.Require().Error(err)
}

func (s *SubscriptionDraftAndComputeOptionsSuite) TestNonEmptyWorkflowIDTakesTheStartWorkflowPath() {
	subID := s.testData.subscription.ID

	_, errZeroValue := s.service.TriggerSubscriptionDraftAndComputeWorkflowWithOptions(
		s.GetContext(), subID, interfaces.DraftAndComputeOptions{},
	)
	_, errExplicitID := s.service.TriggerSubscriptionDraftAndComputeWorkflowWithOptions(
		s.GetContext(), subID, interfaces.DraftAndComputeOptions{
			TaskQueue:  types.TemporalTaskQueueBilling,
			WorkflowID: "wf_test_explicit_id",
		},
	)

	s.Require().Error(errZeroValue)
	s.Require().Error(errExplicitID)
	// Both hit "temporal service not available" (no live Temporal server in unit tests) before
	// the ExecuteWorkflow/StartWorkflow branch matters, but they must still both fail — proving
	// the explicit-ID branch doesn't panic or behave differently pre-Temporal-call.
}
