package interceptor

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

func requestIDActivity(ctx context.Context) ([]string, error) {
	return []string{
		types.GetRequestID(ctx),
		activity.GetInfo(ctx).WorkflowExecution.RunID,
	}, nil
}

func TestRequestIDInterceptor_SetsRunIDAsRequestID(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	env.SetWorkerOptions(worker.Options{
		Interceptors: []interceptor.WorkerInterceptor{NewRequestIDInterceptor()},
	})
	env.RegisterActivity(requestIDActivity)

	val, err := env.ExecuteActivity(requestIDActivity)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	if err := val.Get(&got); err != nil {
		t.Fatal(err)
	}

	requestID, runID := got[0], got[1]
	if runID == "" {
		t.Fatal("test environment gave the activity no run ID")
	}
	if requestID != runID {
		t.Errorf("request ID = %q, want the run ID %q", requestID, runID)
	}
}
