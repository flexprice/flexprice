package events

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRunTriggersOnlyProvider(t *testing.T) {
	ran := map[types.SecretProvider]int{}
	trigger := func(p types.SecretProvider) providerTrigger {
		return providerTrigger{p, func() error { ran[p]++; return nil }}
	}
	triggers := []providerTrigger{
		trigger(types.SecretProviderStripe),
		trigger(types.SecretProviderRazorpay),
		trigger(types.SecretProviderChargebee),
	}

	t.Run("a push for one connection reaches only its provider", func(t *testing.T) {
		ran = map[types.SecretProvider]int{}
		errs := runTriggers(triggers, resolveDispatchOptions([]DispatchOption{OnlyProvider(types.SecretProviderRazorpay)}))
		require.Empty(t, errs)
		require.Equal(t, map[types.SecretProvider]int{types.SecretProviderRazorpay: 1}, ran)
	})

	t.Run("without a provider every provider is tried", func(t *testing.T) {
		ran = map[types.SecretProvider]int{}
		errs := runTriggers(triggers, resolveDispatchOptions(nil))
		require.Empty(t, errs)
		require.Len(t, ran, 3)
	})

	t.Run("failures are gathered, not stopping the rest", func(t *testing.T) {
		boom := errors.New("boom")
		errs := runTriggers([]providerTrigger{
			{types.SecretProviderStripe, func() error { return boom }},
			{types.SecretProviderRazorpay, func() error { return nil }},
		}, resolveDispatchOptions(nil))
		require.Equal(t, []error{boom}, errs)
	})
}

type invoiceEmailSenderFunc func(ctx context.Context, invoiceID string) error

func (f invoiceEmailSenderFunc) SendAutoInvoiceEmail(ctx context.Context, invoiceID string) error {
	return f(ctx, invoiceID)
}

func TestDispatchInvoiceAutoEmail_MissingDataIsNothingToSend(t *testing.T) {
	cfg := &config.Configuration{Email: config.EmailConfig{Enabled: true, Provider: string(types.EmailProviderLocal)}}
	event := &types.WebhookEvent{Payload: json.RawMessage(`{"invoice_id":"inv_1"}`)}
	send := func(err error) error {
		sender := invoiceEmailSenderFunc(func(context.Context, string) error { return err })
		return DispatchInvoiceAutoEmail(context.Background(), cfg, sender, testLogger(), event, "msg-1")
	}

	require.NoError(t, send(ierr.NewError("invoice not found").Mark(ierr.ErrNotFound)))
	boom := errors.New("boom")
	require.ErrorIs(t, send(boom), boom)
}
