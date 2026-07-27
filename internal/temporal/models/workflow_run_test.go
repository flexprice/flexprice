package models

import (
	"testing"

	enumspb "go.temporal.io/api/enums/v1"

	"github.com/stretchr/testify/require"
)

func TestStartWorkflowOptions_ToSDKOptions_ReusePolicy(t *testing.T) {
	t.Parallel()

	t.Run("zero value maps to unspecified, matching today's behavior for every existing caller", func(t *testing.T) {
		opts := StartWorkflowOptions{ID: "wf_1", TaskQueue: "invoice"}
		sdk := opts.ToSDKOptions()
		require.Equal(t, enumspb.WORKFLOW_ID_REUSE_POLICY_UNSPECIFIED, sdk.WorkflowIDReusePolicy)
	})

	t.Run("explicit policy is passed through", func(t *testing.T) {
		opts := StartWorkflowOptions{
			ID:                    "wf_1",
			TaskQueue:             "billing",
			WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		}
		sdk := opts.ToSDKOptions()
		require.Equal(t, enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE, sdk.WorkflowIDReusePolicy)
	})
}
