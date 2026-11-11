package export

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
)

func TestStartHeartbeat_PingsWhileRunning(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	var beats atomic.Int32
	env.SetOnActivityHeartbeatListener(func(_ *activity.Info, _ converter.EncodedValues) { beats.Add(1) })

	slow := func(ctx context.Context) error {
		stop := startHeartbeat(ctx, 5*time.Millisecond)
		defer stop()
		time.Sleep(100 * time.Millisecond)
		return nil
	}
	env.RegisterActivity(slow)
	_, err := env.ExecuteActivity(slow)

	require.NoError(t, err)
	require.GreaterOrEqual(t, beats.Load(), int32(1))
}

func TestStartHeartbeat_NoopOutsideActivity(t *testing.T) {
	stop := startHeartbeat(context.Background(), time.Millisecond)
	stop()
}
