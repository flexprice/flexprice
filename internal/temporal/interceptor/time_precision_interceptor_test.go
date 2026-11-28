package interceptor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

type timePrecisionInput struct {
	At     time.Time
	EndsAt *time.Time
	Items  []timePrecisionItem
}

type timePrecisionItem struct {
	At time.Time
}

type timePrecisionOutput struct {
	Value   timePrecisionInput
	Pointer *timePrecisionInput
	Bare    time.Time
}

func timePrecisionActivity(_ context.Context, value timePrecisionInput, pointer *timePrecisionInput, bare time.Time) (*timePrecisionOutput, error) {
	return &timePrecisionOutput{
		Value:   value,
		Pointer: pointer,
		Bare:    bare,
	}, nil
}

func TestTimePrecisionInterceptor_TruncatesActivityArgs(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	env.SetWorkerOptions(worker.Options{
		Interceptors: []interceptor.WorkerInterceptor{NewTimePrecisionInterceptor()},
	})
	env.RegisterActivity(timePrecisionActivity)

	at := time.Date(2026, 1, 10, 12, 0, 0, 123456789, time.UTC)
	endsAt := time.Date(2026, 1, 10, 13, 0, 0, 999999999, time.UTC)
	input := timePrecisionInput{
		At:     at,
		EndsAt: &endsAt,
		Items:  []timePrecisionItem{{At: at}},
	}

	val, err := env.ExecuteActivity(timePrecisionActivity, input, &input, at)
	require.NoError(t, err)

	var got timePrecisionOutput
	require.NoError(t, val.Get(&got))

	wantAt := time.Date(2026, 1, 10, 12, 0, 0, 123456000, time.UTC)
	wantEndsAt := time.Date(2026, 1, 10, 13, 0, 0, 999999000, time.UTC)
	for _, in := range []timePrecisionInput{got.Value, *got.Pointer} {
		require.True(t, in.At.Equal(wantAt), "At = %s", in.At)
		require.True(t, in.EndsAt.Equal(wantEndsAt), "EndsAt = %s", in.EndsAt)
		require.True(t, in.Items[0].At.Equal(wantAt), "Items[0].At = %s", in.Items[0].At)
	}
	require.True(t, got.Bare.Equal(wantAt), "Bare = %s", got.Bare)
}
