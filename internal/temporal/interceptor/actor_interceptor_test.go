package interceptor

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

type actorProbe struct {
	Actor  types.Actor
	Source types.Source
}

func probeActorActivity(ctx context.Context) (actorProbe, error) {
	return actorProbe{Actor: types.GetActor(ctx), Source: types.GetSource(ctx)}, nil
}

func CreditGrantProcessingWorkflow(ctx workflow.Context) (actorProbe, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
	var probe actorProbe
	err := workflow.ExecuteActivity(ctx, probeActorActivity).Get(ctx, &probe)
	return probe, err
}

func TestActorInterceptorStampsWorkflowActor(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{Interceptors: []interceptor.WorkerInterceptor{NewActorInterceptor()}})
	env.RegisterWorkflow(CreditGrantProcessingWorkflow)
	env.RegisterActivityWithOptions(probeActorActivity, activity.RegisterOptions{Name: "probeActorActivity"})

	env.ExecuteWorkflow(CreditGrantProcessingWorkflow)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var probe actorProbe
	if err := env.GetWorkflowResult(&probe); err != nil {
		t.Fatal(err)
	}
	want := types.WorkflowActor("CreditGrantProcessingWorkflow")
	if probe.Actor != want {
		t.Fatalf("actor = %+v, want %+v", probe.Actor, want)
	}
	if probe.Source != types.SourceWorkflow {
		t.Fatalf("source = %q, want %q", probe.Source, types.SourceWorkflow)
	}
}
