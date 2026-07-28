package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/stretchr/testify/suite"
)

type SubscriptionDraftAndComputeOptionsSuite struct {
	SubscriptionServiceSuite
}

func TestSubscriptionDraftAndComputeOptions(t *testing.T) {
	suite.Run(t, new(SubscriptionDraftAndComputeOptionsSuite))
}

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
		s.GetContext(), "", interfaces.DraftAndComputeOptions{},
	)
	s.Require().Error(err)
}
