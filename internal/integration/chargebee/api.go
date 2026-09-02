package chargebee

import (
	"context"

	chargebeeSDK "github.com/chargebee/chargebee-go/v3"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// env resolves the calling tenant's Chargebee environment.
func (c *Client) env(ctx context.Context) (chargebeeSDK.Environment, error) {
	cfg, err := c.GetChargebeeConfig(ctx)
	if err != nil {
		return chargebeeSDK.Environment{}, err
	}
	return chargebeeSDK.Environment{Key: cfg.APIKey, SiteName: cfg.Site}, nil
}

// IsNoPaymentMethod reports the "no valid card on file" case. Verified live:
// collect_payment returns HTTP 400 payment_method_not_present when the customer
// has no usable source. Callers map this to charged=false rather than treating
// it as a hard failure.
func IsNoPaymentMethod(err error) bool {
	if e, ok := err.(*chargebeeSDK.Error); ok {
		return e.APIErrorCode == "payment_method_not_present"
	}
	return false
}

// wrapAPIError keeps the Chargebee *Error as the cause so IsNoPaymentMethod and
// any other api_error_code branch still works on the returned error.
func wrapAPIError(err error, hint string) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*chargebeeSDK.Error); ok {
		return err
	}
	return ierr.WithError(err).WithHint(hint).Mark(ierr.ErrHTTPClient)
}

// missingPayload reports a 2xx response whose expected object was absent.
func missingPayload(object string) error {
	return ierr.NewErrorf("chargebee response missing %s", object).
		WithHintf("Chargebee returned an unexpected %s payload", object).
		Mark(ierr.ErrInternal)
}
