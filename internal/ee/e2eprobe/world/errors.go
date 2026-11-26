package world

import (
	"errors"
	"net/http"
	"strings"

	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// IsNotFound matches both shapes the SDK uses for a 404: the typed
// ErrorResponse of explicit 404 branches and the generic APIError.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var errResp *sdkerrors.ErrorResponse
	if errors.As(err, &errResp) {
		if errResp.HTTPStatusCode != nil && *errResp.HTTPStatusCode == http.StatusNotFound {
			return true
		}
		if errResp.Code != nil && *errResp.Code == types.ErrorCodeNotFound {
			return true
		}
	}
	var apiErr *sdkerrors.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// IsAlreadyExists matches a 409 or an already_exists code, for create-only idempotency.
func IsAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var errResp *sdkerrors.ErrorResponse
	if errors.As(err, &errResp) {
		if errResp.HTTPStatusCode != nil && *errResp.HTTPStatusCode == http.StatusConflict {
			return true
		}
		if errResp.Code != nil && *errResp.Code == types.ErrorCodeAlreadyExists {
			return true
		}
	}
	var apiErr *sdkerrors.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusConflict || strings.Contains(apiErr.Body, `"code":"already_exists"`)
	}
	return false
}
