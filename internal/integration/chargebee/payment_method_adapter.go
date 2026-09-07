package chargebee

import (
	"context"
	"time"

	"github.com/chargebee/chargebee-go/v3/enum"
	paymentSourceModel "github.com/chargebee/chargebee-go/v3/models/paymentsource"
	paymentSourceEnum "github.com/chargebee/chargebee-go/v3/models/paymentsource/enum"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type PaymentMethodAdapter struct {
	Client      ChargebeeClient
	CustomerSvc *CustomerService
	Logger      *logger.Logger
}

func (a *PaymentMethodAdapter) ListSavedMethods(ctx context.Context, flexCustomerID string) ([]interfaces.ProviderPaymentMethod, error) {
	cbCustomerID, err := a.CustomerSvc.GetChargebeeCustomerID(ctx, flexCustomerID)
	if err != nil {
		// Not yet synced to Chargebee simply means nothing is saved.
		a.Logger.Info(ctx, "no chargebee customer for saved payment methods",
			"customer_id", flexCustomerID, "error", err)
		return nil, nil
	}

	sources, err := a.Client.ListPaymentSources(ctx, cbCustomerID)
	if err != nil {
		return nil, err
	}

	primary := ""
	if cust, err := a.Client.RetrieveCustomer(ctx, cbCustomerID); err == nil {
		primary = cust.PrimaryPaymentSourceId
	} else {
		// Losing the primary flag is not worth failing the whole listing over; the
		// caller sees methods with no default rather than an error.
		a.Logger.Info(ctx, "could not read chargebee primary payment source",
			"customer_id", flexCustomerID, "error", err)
	}

	out := make([]interfaces.ProviderPaymentMethod, 0, len(sources))
	for _, src := range sources {
		method, ok := toProviderPaymentMethod(src, primary)
		if !ok {
			// A method type with no FlexPrice equivalent is dropped rather than
			// mapped to something it is not.
			a.Logger.Info(ctx, "skipping unmappable chargebee payment source",
				"customer_id", flexCustomerID, "payment_source_id", src.Id, "type", src.Type)
			continue
		}
		out = append(out, method)
	}
	return out, nil
}

func (a *PaymentMethodAdapter) SetDefaultSavedMethod(ctx context.Context, flexCustomerID, gatewayMethodID string) error {
	cbCustomerID, err := a.ownedSourceCustomer(ctx, flexCustomerID, gatewayMethodID)
	if err != nil {
		return err
	}

	return a.Client.AssignPaymentRole(ctx, cbCustomerID, gatewayMethodID, enum.RolePrimary)
}

func (a *PaymentMethodAdapter) DeleteSavedMethod(ctx context.Context, flexCustomerID, gatewayMethodID string) error {
	if _, err := a.ownedSourceCustomer(ctx, flexCustomerID, gatewayMethodID); err != nil {
		return err
	}

	return a.Client.DeletePaymentSource(ctx, gatewayMethodID)
}

func (a *PaymentMethodAdapter) CreateSetupLink(ctx context.Context, req interfaces.SetupLinkRequest) (*interfaces.SetupLinkResponse, error) {
	// The page needs a Chargebee customer, so create one now if the customer has
	// never been synced — otherwise there is nothing to attach the card to.
	if _, err := a.CustomerSvc.EnsureCustomerSyncedToChargebee(ctx, req.CustomerID); err != nil {
		return nil, err
	}
	cbCustomerID, err := a.CustomerSvc.GetChargebeeCustomerID(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}

	cfg, err := a.Client.GetChargebeeConfig(ctx)
	if err != nil {
		return nil, err
	}

	page, err := a.Client.CreateManagePaymentSourcesPage(ctx, cbCustomerID, req.ReturnURL, cfg.GatewayAccountID)
	if err != nil {
		return nil, err
	}
	if page.Url == "" {
		return nil, missingPayload("hosted page url")
	}

	resp := &interfaces.SetupLinkResponse{URL: page.Url, ProviderSessionID: page.Id}
	if page.ExpiresAt > 0 {
		expiresAt := time.Unix(page.ExpiresAt, 0).UTC()
		resp.ExpiresAt = &expiresAt
	}
	return resp, nil
}

// ownedSourceCustomer resolves the customer and verifies the source belongs to
// them. This is the only protection there is: Chargebee's delete and
// assign_payment_role take a bare source id, so without it any customer could act
// on another customer's card by guessing an id.
func (a *PaymentMethodAdapter) ownedSourceCustomer(ctx context.Context, flexCustomerID, gatewayMethodID string) (string, error) {
	if gatewayMethodID == "" {
		return "", ierr.NewError("payment method id is required").
			WithHint("Specify which saved payment method to act on").
			Mark(ierr.ErrValidation)
	}

	cbCustomerID, err := a.CustomerSvc.GetChargebeeCustomerID(ctx, flexCustomerID)
	if err != nil {
		return "", err
	}

	sources, err := a.Client.ListPaymentSources(ctx, cbCustomerID)
	if err != nil {
		return "", err
	}
	for _, src := range sources {
		if src != nil && src.Id == gatewayMethodID {
			return cbCustomerID, nil
		}
	}

	// Deliberately not-found rather than permission-denied: telling a caller the id
	// exists but belongs to someone else confirms it exists.
	return "", ierr.NewError("payment method not found").
		WithHint("This payment method does not belong to the customer").
		Mark(ierr.ErrNotFound)
}

// toProviderPaymentMethod maps one Chargebee source. ok=false means the type has no
// FlexPrice equivalent and the method should be dropped from the listing.
func toProviderPaymentMethod(src *paymentSourceModel.PaymentSource, primaryID string) (interfaces.ProviderPaymentMethod, bool) {
	if src == nil || src.Deleted {
		return interfaces.ProviderPaymentMethod{}, false
	}

	method, ok := methodTypeFor(src.Type)
	if !ok {
		return interfaces.ProviderPaymentMethod{}, false
	}

	out := interfaces.ProviderPaymentMethod{
		GatewayMethodID:  src.Id,
		Method:           method,
		CreatedAt:        time.Unix(src.CreatedAt, 0).UTC(),
		IsDefault:        src.Id == primaryID && primaryID != "",
		Active:           src.Status == paymentSourceEnum.StatusValid || src.Status == paymentSourceEnum.StatusExpiring,
		GatewayAccountID: src.GatewayAccountId,
		ProviderMetadata: map[string]string{
			"chargebee_payment_source_status": string(src.Status),
			"chargebee_gateway":               string(src.Gateway),
		},
	}

	if src.Card != nil {
		out.Card = &interfaces.ProviderCardDetails{
			Brand:    string(src.Card.Brand),
			Last4:    src.Card.Last4,
			ExpMonth: int(src.Card.ExpiryMonth),
			ExpYear:  int(src.Card.ExpiryYear),
		}
	}

	return out, true
}

// methodTypeFor maps Chargebee's payment source type onto ours. Chargebee has far
// more types than PaymentMethodType has members; the unmapped ones are dropped
// rather than forced into a member that means something else.
func methodTypeFor(t enum.Type) (types.PaymentMethodType, bool) {
	switch t {
	case enum.TypeCard, enum.TypeApplePay, enum.TypeGooglePay:
		// Wallet-presented cards are still cards: they carry card details and are
		// charged as one.
		return types.PaymentMethodTypeCard, true
	case enum.TypeDirectDebit:
		return types.PaymentMethodTypeACH, true
	case enum.TypeUpi:
		return types.PaymentMethodTypeUPI, true
	default:
		return "", false
	}
}

// paymentMethodTypeFor maps the method Chargebee reports on a settled transaction.
// This is a different Chargebee enum from the payment source type methodTypeFor
// reads, so the two switches cannot be shared even though the members overlap.
func paymentMethodTypeFor(m enum.PaymentMethod) (types.PaymentMethodType, bool) {
	switch m {
	case enum.PaymentMethodCard, enum.PaymentMethodApplePay, enum.PaymentMethodGooglePay:
		return types.PaymentMethodTypeCard, true
	case enum.PaymentMethodDirectDebit, enum.PaymentMethodAchCredit:
		return types.PaymentMethodTypeACH, true
	case enum.PaymentMethodUpi:
		return types.PaymentMethodTypeUPI, true
	default:
		return "", false
	}
}
