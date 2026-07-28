package types

import (
	"strings"
	"testing"

	cockroachdberrors "github.com/cockroachdb/errors"
)

func TestIntegrationEntityType_Validate(t *testing.T) {
	if err := IntegrationEntityTypeSubscriptionLineItem.Validate(); err != nil {
		t.Fatalf("expected valid entity type, got error: %v", err)
	}

	err := IntegrationEntityType("bogus").Validate()
	if err == nil {
		t.Fatal("expected error for invalid entity type, got nil")
	}

	hints := cockroachdberrors.GetAllHints(err)
	if len(hints) == 0 {
		t.Fatal("expected error to carry a hint, got none")
	}
	if !strings.Contains(hints[0], "bogus") {
		t.Fatalf("expected hint to include the offending value, got %q", hints[0])
	}
}
