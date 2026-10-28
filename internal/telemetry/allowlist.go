package telemetry

func isTrackedEventName(name string) bool {
	switch name {
	case "customer.created",
		"customer.deleted",
		"subscription.created",
		"subscription.activated",
		"subscription.cancelled",
		"credit_note.created",
		"invoice.update.finalized",
		"payment.success",
		"payment.failed",
		"refund.succeeded",
		"wallet.created":
		return true
	default:
		return false
	}
}
