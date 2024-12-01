package integrations

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/usagerecord"
)

type StripeIntegration struct {
	eventService service.EventService
	meterRepo    meter.Repository
}

func NewStripeIntegration(
	eventService service.EventService,
	meterRepo meter.Repository,
	stripeSecretKey string,
) *StripeIntegration {
	// Set the Stripe API key globally
	stripe.Key = stripeSecretKey

	return &StripeIntegration{
		eventService: eventService,
		meterRepo:    meterRepo,
	}
}

type SyncUsageParams struct {
	MeterID                  string
	ExternalCustomerID       string
	StartTime                time.Time
	EndTime                  time.Time
	StripeSubscriptionItemID string
	TenantID                 string
}

func (s *StripeIntegration) SyncUsageToStripe(ctx context.Context, params *SyncUsageParams) error {
	ctx = context.WithValue(ctx, types.CtxTenantID, params.TenantID)

	usage, err := s.eventService.GetUsageByMeter(ctx, &dto.GetUsageByMeterRequest{
		MeterID:            params.MeterID,
		ExternalCustomerID: params.ExternalCustomerID,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
	})
	if err != nil {
		return fmt.Errorf("failed to get usage: %w", err)
	}

	var quantity int64
	if usage.Value == nil {
		if len(usage.Results) > 0 {
			resultValue := usage.Results[0].Value
			switch v := resultValue.(type) {
			case float64:
				quantity = int64(v)
			case int64:
				quantity = v
			case uint64:
				quantity = int64(v)
			default:
				return fmt.Errorf("unsupported usage result value type: %T", resultValue)
			}
		} else {
			return fmt.Errorf("usage results are empty for MeterID: %s, ExternalCustomerID: %s", params.MeterID, params.ExternalCustomerID)
		}
	} else {
		switch v := usage.Value.(type) {
		case float64:
			quantity = int64(v)
		case int64:
			quantity = v
		case uint64:
			quantity = int64(v)
		default:
			return fmt.Errorf("unsupported usage value type: %T", usage.Value)
		}
	}
	stripeParams := &stripe.UsageRecordParams{
		SubscriptionItem: stripe.String(params.StripeSubscriptionItemID),
		Quantity:         stripe.Int64(quantity),
		Timestamp:        stripe.Int64(params.EndTime.Unix()),
		Action:           stripe.String(string(stripe.UsageRecordActionSet)),
	}

	_, err = usagerecord.New(stripeParams)
	if err != nil {
		return fmt.Errorf("failed to create stripe usage record: %w", err)
	}

	return nil
}
