package client

import (
	"context"
	"errors"
	"testing"

	"github.com/cenkalti/backoff/v4"
	"go.temporal.io/api/serviceerror"
	sdkclient "go.temporal.io/sdk/client"
)

func TestDialTemporalClientRetriesUnavailableErrors(t *testing.T) {
	attempts := 0
	dial := func(context.Context, sdkclient.Options) (sdkclient.Client, error) {
		attempts++
		if attempts < 3 {
			return nil, serviceerror.NewUnavailable("frontend is not healthy yet")
		}
		return nil, nil
	}

	_, err := dialTemporalClient(
		context.Background(),
		sdkclient.Options{},
		dial,
		backoff.WithMaxRetries(backoff.NewConstantBackOff(0), 3),
	)
	if err != nil {
		t.Fatalf("dialTemporalClient() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("dialTemporalClient() attempts = %d, want 3", attempts)
	}
}

func TestDialTemporalClientDoesNotRetryPermanentErrors(t *testing.T) {
	attempts := 0
	permanentErr := errors.New("invalid client configuration")
	dial := func(context.Context, sdkclient.Options) (sdkclient.Client, error) {
		attempts++
		return nil, permanentErr
	}

	_, err := dialTemporalClient(
		context.Background(),
		sdkclient.Options{},
		dial,
		backoff.WithMaxRetries(backoff.NewConstantBackOff(0), 3),
	)
	if !errors.Is(err, permanentErr) {
		t.Fatalf("dialTemporalClient() error = %v, want %v", err, permanentErr)
	}
	if attempts != 1 {
		t.Fatalf("dialTemporalClient() attempts = %d, want 1", attempts)
	}
}
