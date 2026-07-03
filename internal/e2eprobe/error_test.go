package e2eprobe

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestErrorf_EnrichesAPIError(t *testing.T) {
	apiErr := sdkerrors.NewAPIError("bad request", 400, `{"detail":"missing field"}`, nil)
	err := Errorf(map[string]string{"customer_id": "cust_1"}, "delete customer: %w", apiErr)

	attrs := AttributesFrom(err)
	if attrs == nil {
		t.Fatal("expected CheckError attributes, got nil")
	}
	if attrs["status_code"] != "400" {
		t.Errorf("expected status_code=400, got %q", attrs["status_code"])
	}
	if attrs["error_body"] != `{"detail":"missing field"}` {
		t.Errorf("expected error_body to carry the JSON, got %q", attrs["error_body"])
	}
	if attrs["customer_id"] != "cust_1" {
		t.Errorf("caller attribute lost; got %q", attrs["customer_id"])
	}
}

func TestErrorf_EnrichesErrorsErrorResponse(t *testing.T) {
	// Mimic the production case: a typed response with empty fields → Error()
	// marshals to literal "{}". The status code lives on HTTPMeta.
	eer := &sdkerrors.ErrorsErrorResponse{
		HTTPMeta: types.HTTPMetadata{
			Response: &http.Response{StatusCode: 500},
		},
	}
	err := Errorf(map[string]string{"event_name": "e2eprobe_latest"}, "analytics for x/y: %w", eer)

	attrs := AttributesFrom(err)
	if attrs == nil {
		t.Fatal("expected attributes, got nil")
	}
	if attrs["status_code"] != "500" {
		t.Errorf("expected status_code=500, got %q", attrs["status_code"])
	}
	if attrs["error_body"] != "{}" {
		t.Errorf("expected error_body=%q (the bare {} body we've been chasing), got %q",
			"{}", attrs["error_body"])
	}
	if attrs["event_name"] != "e2eprobe_latest" {
		t.Errorf("caller attribute lost; got %q", attrs["event_name"])
	}
}

func TestErrorf_PlainErrorIsUntouched(t *testing.T) {
	// Non-SDK errors should not get spurious status_code attributes.
	err := Errorf(map[string]string{"customer_id": "cust_1"}, "parse %q: %w", "bad", fmt.Errorf("invalid input"))

	attrs := AttributesFrom(err)
	if attrs == nil {
		t.Fatal("expected attributes, got nil")
	}
	if _, ok := attrs["status_code"]; ok {
		t.Errorf("status_code should not be set for non-SDK errors, got %q", attrs["status_code"])
	}
	if _, ok := attrs["error_body"]; ok {
		t.Errorf("error_body should not be set for non-SDK errors, got %q", attrs["error_body"])
	}
}

func TestErrorf_CallerAttributeWinsOnConflict(t *testing.T) {
	// If the caller already set status_code, enrichment must not overwrite it.
	apiErr := sdkerrors.NewAPIError("bad", 400, "{}", nil)
	err := Errorf(map[string]string{"status_code": "caller-set"}, "wrap: %w", apiErr)

	attrs := AttributesFrom(err)
	if attrs["status_code"] != "caller-set" {
		t.Errorf("caller attribute overwritten; got %q, want caller-set", attrs["status_code"])
	}
}

func TestErrorf_NilAttrsIsHandled(t *testing.T) {
	apiErr := sdkerrors.NewAPIError("bad", 400, "{}", nil)
	err := Errorf(nil, "wrap: %w", apiErr)

	attrs := AttributesFrom(err)
	if attrs == nil {
		t.Fatal("expected attributes when caller passed nil, got nil")
	}
	if attrs["status_code"] != "400" {
		t.Errorf("expected status_code=400 from enrichment, got %q", attrs["status_code"])
	}
}

func TestAttributesFrom_NoCheckErrorReturnsNil(t *testing.T) {
	if attrs := AttributesFrom(errors.New("plain")); attrs != nil {
		t.Errorf("expected nil, got %v", attrs)
	}
	if attrs := AttributesFrom(nil); attrs != nil {
		t.Errorf("expected nil for nil err, got %v", attrs)
	}
}
