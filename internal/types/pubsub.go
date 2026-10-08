package types

import "github.com/flexprice/flexprice/internal/pubsub"

type WalletBalanceAlertPubSub struct {
	pubsub.PubSub
}

// IntegrationEventsPubSub is a named wrapper around pubsub.PubSub so FX can
// inject it independently from the webhook consumer's pubsub.PubSub.
type IntegrationEventsPubSub struct {
	pubsub.PubSub
}
