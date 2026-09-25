package zoho

import (
	"context"
	"testing"

	customerDomain "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These cover the defect where a USD invoice reached Zoho as INR: the contact was created
// without a currency, so Zoho locked it to the org base currency and reinterpreted every
// amount sent to it.

func TestCreateContactCarriesCurrency(t *testing.T) {
	client := &fakeContactClient{}
	svc := newTestCustomerService(client, &writableMappingRepo{})

	_, err := svc.GetOrCreateZohoCustomer(context.Background(), indianCustomer(), "usd")
	require.NoError(t, err)

	require.NotNil(t, client.createReq)
	assert.Equal(t, "cur_usd", client.createReq.CurrencyID,
		"a contact created without a currency is permanently locked to the org base currency")
	assert.Equal(t, []string{"usd"}, client.currencyCodes,
		"the contact's currency must come from the invoice being synced")
}

func TestCreateContactFailsWhenCurrencyUnresolvable(t *testing.T) {
	client := &fakeContactClient{
		currencyIDErr: ierr.NewError("currency USD is not enabled in Zoho Books").
			Mark(ierr.ErrValidation),
	}
	svc := newTestCustomerService(client, &writableMappingRepo{})

	_, err := svc.GetOrCreateZohoCustomer(context.Background(), indianCustomer(), "usd")

	require.Error(t, err)
	assert.Equal(t, 0, client.createCalls,
		"no contact may be created when its currency cannot be resolved")
}

func TestMappedContactSkipsCurrencyLookup(t *testing.T) {
	client := &fakeContactClient{}
	repo := &writableMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{{
			EntityID:         "cust_1",
			ProviderEntityID: "zoho_contact_1",
		}},
	}
	svc := newTestCustomerService(client, repo)

	id, err := svc.GetOrCreateZohoCustomer(context.Background(), indianCustomer(), "usd")

	require.NoError(t, err)
	assert.Equal(t, "zoho_contact_1", id)
	assert.Empty(t, client.currencyCodes,
		"an existing contact's currency is immutable, so it must not be looked up")
}

func TestContactUpdateNeverSendsCurrency(t *testing.T) {
	client := &fakeContactClient{}
	repo := &writableMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{{
			EntityID:         "cust_1",
			ProviderEntityID: "zoho_contact_1",
		}},
	}
	svc := newTestCustomerService(client, repo)

	require.NoError(t, svc.SyncCustomerUpdate(context.Background(), indianCustomer()))

	require.Equal(t, 1, client.updateCalls)
	assert.Empty(t, client.currencyCodes,
		"Zoho rejects a currency change once a contact has transactions")
}

func TestSyncInvoiceSendsCurrencyIDAndLeavesAmountsAlone(t *testing.T) {
	// The original bug: $5.00 arriving in Zoho as ₹5.00, then taxed as INR.
	inv := buildTestInvoice("usd", "0", "0", "", []testLineItem{
		{name: "Tokens", priceID: "price_1", amount: "5.00", lineDisc: "0", invDisc: "0"},
	})
	svc, client := newSyncTestService(inv, nil)

	_, err := svc.SyncInvoiceToZoho(context.Background(), ZohoInvoiceSyncRequest{InvoiceID: "inv_1"})
	require.NoError(t, err)

	req := client.createInvoiceReq
	require.NotNil(t, req)

	assert.Equal(t, "cur_usd", req.CurrencyID,
		"Zoho identifies invoice currency by id; without it the invoice inherits the contact's")

	require.Len(t, req.LineItems, 1)
	assert.Truef(t, dec("5.00").Equal(req.LineItems[0].Rate),
		"line rate must be sent unconverted, got %s", req.LineItems[0].Rate)
}

func TestCustomerWithoutCurrencyIsRejected(t *testing.T) {
	client := &fakeContactClient{
		currencyIDErr: ierr.NewError("currency code is empty").Mark(ierr.ErrValidation),
	}
	svc := newTestCustomerService(client, &writableMappingRepo{})

	c := &customerDomain.Customer{ID: "cust_9", Name: "Acme Inc"}
	_, err := svc.GetOrCreateZohoCustomer(context.Background(), c, "")

	require.Error(t, err)
	assert.Equal(t, 0, client.createCalls)
}
