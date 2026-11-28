package telemetry

import "testing"

func TestTrackedEventNames(t *testing.T) {
	tracked := []string{
		"customer.created",
		"customer.deleted",
		"subscription.created",
		"subscription.activated",
		"subscription.cancelled",
		"credit_note.created",
		"invoice.update.finalized",
		"payment.success",
		"payment.failed",
		"refund.succeeded",
		"wallet.created",
	}
	for _, name := range tracked {
		if !isTrackedEventName(name) {
			t.Errorf("expected %q to be tracked", name)
		}
	}

	untracked := []string{
		"event.rejected",
		"feature.created",
		"entitlement.created",
		"checkout.session.completed",
		"wallet.ongoing_balance.updated",
		"invoice.update",
		"payment.created",
		"",
	}
	for _, name := range untracked {
		if isTrackedEventName(name) {
			t.Errorf("expected %q to not be tracked", name)
		}
	}
}
