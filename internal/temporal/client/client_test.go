package client

import (
	"testing"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
)

func TestNewTemporalClientIsLazy(t *testing.T) {
	temporalClient, err := NewTemporalClient(&models.ClientOptions{
		Address:   "127.0.0.1:1",
		Namespace: "default",
	}, logger.NewNoopLogger())
	if err != nil {
		t.Fatalf("NewTemporalClient() error = %v", err)
	}
	if temporalClient.GetRawClient() == nil {
		t.Fatal("NewTemporalClient() returned a nil SDK client")
	}
}
