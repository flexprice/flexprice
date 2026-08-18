package client

import (
	"net/http"
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/exitcode"
)

func TestAPIError_RendersWhatWhyNext(t *testing.T) {
	// Shape verified against the live API.
	body := []byte(`{"code":"not_found","message":"Customer with ID cust_missing was not found",` +
		`"http_status_code":404,"details":{"customer_id":"cust_missing"}}`)
	err := NewAPIError(http.StatusNotFound, body, "GET", "/v1/customers/cust_missing")

	got := err.Error()
	for _, want := range []string{"was not found", "customer_id", "cust_missing"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
	if err.ExitCode() != exitcode.NotFound {
		t.Errorf("ExitCode() = %d, want %d", err.ExitCode(), exitcode.NotFound)
	}
}

func TestAPIError_ExitCodeByStatus(t *testing.T) {
	cases := map[int]int{
		http.StatusUnauthorized:        exitcode.Auth,
		http.StatusForbidden:           exitcode.Auth,
		http.StatusNotFound:            exitcode.NotFound,
		http.StatusTooManyRequests:     exitcode.RateLimited,
		http.StatusBadRequest:          exitcode.Usage,
		http.StatusInternalServerError: exitcode.Generic,
	}
	for status, want := range cases {
		err := NewAPIError(status, []byte(`{}`), "GET", "/v1/x")
		if got := err.ExitCode(); got != want {
			t.Errorf("status %d: ExitCode() = %d, want %d", status, got, want)
		}
	}
}

// The auth middleware returns a bare string under "error", unlike every other path.
func TestAPIError_HandlesBareErrorString(t *testing.T) {
	err := NewAPIError(http.StatusUnauthorized, []byte(`{"error":"Unauthorized"}`), "GET", "/v1/customers")
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("Error() = %q, want it to surface the bare error string", err.Error())
	}
	if err.ExitCode() != exitcode.Auth {
		t.Errorf("ExitCode() = %d, want %d", err.ExitCode(), exitcode.Auth)
	}
}

func TestAPIError_UnparseableBodyStillRenders(t *testing.T) {
	err := NewAPIError(http.StatusBadGateway, []byte("<html>gateway</html>"), "GET", "/v1/x")
	if err.Error() == "" {
		t.Fatal("Error() is empty for a non-JSON body")
	}
}

// A non-string value inside details must not fail the whole envelope decode and
// take the message and code with it.
func TestAPIError_NonStringDetailValuePreservesMessage(t *testing.T) {
	body := []byte(`{"code":"validation_error","message":"Request validation failed",` +
		`"http_status_code":400,"details":{"count":5,"field":"currency"}}`)
	err := NewAPIError(http.StatusBadRequest, body, "POST", "/v1/subscriptions")

	if !strings.Contains(err.Error(), "Request validation failed") {
		t.Errorf("Error() = %q, message was lost", err.Error())
	}
	if err.Code != "validation_error" {
		t.Errorf("Code = %q, want validation_error", err.Code)
	}
	for _, want := range []string{"count: 5", "field: currency"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Error() = %q, missing %q", err.Error(), want)
		}
	}
}

// A type mismatch on details (array instead of object) must not discard code and
// message that decoded successfully before the decoder reached the bad field.
func TestAPIError_DetailsTypeMismatchPreservesCodeAndMessage(t *testing.T) {
	body := []byte(`{"code":"validation_error","message":"plan_id is required","details":[]}`)
	err := NewAPIError(http.StatusBadRequest, body, "POST", "/v1/plans")

	if err.Code != "validation_error" {
		t.Errorf("Code = %q, want validation_error", err.Code)
	}
	if err.Message != "plan_id is required" {
		t.Errorf("Message = %q, want %q", err.Message, "plan_id is required")
	}
}
