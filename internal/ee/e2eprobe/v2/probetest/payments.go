package probetest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// Payments models checkout sessions and saved methods. Sessions start pending
// with a URL; reads return them completed unless SessionOnGet says otherwise.
type Payments struct {
	mu     sync.Mutex
	nextID int

	Sessions map[string]*types.CheckoutSessionResponse
	// Start, when set, replaces the default pending session for a checkout
	// start; action is "topup", "invoice", "subscription", "modify" or "addon".
	Start           func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error)
	SessionOnGet    func(s *types.CheckoutSessionResponse) *types.CheckoutSessionResponse
	GetSessionCalls int
	Cancelled       []string
	CancelStatus    types.CheckoutStatus
	PaymentStatus   types.PaymentStatus

	SavedMethods     []e2eprobe.SavedPaymentMethod
	ListErr          error
	SetupURL         string
	PortalAddURL     string
	PortalAddErr     error
	PortalDeleteErr  error
	Defaults         []string
	Deleted          []string
	IgnoreSetDefault bool

	OnModify          func(subID string, req types.ExecuteSubscriptionModifyRequest)
	OnCreditNote      func(req types.CreateCreditNoteRequest)
	Addons            map[string]string // lookup key → addon id
	AddonAssociations []types.AddonAssociationResponse
	CreditNotes       []types.CreateCreditNoteRequest
	Refunds           []types.RefundResponse
	// Ungated makes "modify" or "addon" apply without a checkout session.
	Ungated map[string]bool
}

func (f *Payments) startSession(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
	f.mu.Lock()
	start := f.Start
	f.mu.Unlock()
	var s *types.CheckoutSessionResponse
	if start != nil {
		var err error
		if s, err = start(action, cfg); err != nil {
			return nil, err
		}
	} else {
		s = PendingSession()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	if s.ID == nil {
		s.ID = Ptr(fmt.Sprintf("cs_%d", f.nextID))
	}
	if s.CheckoutPaymentID == nil {
		s.CheckoutPaymentID = Ptr(fmt.Sprintf("pay_%d", f.nextID))
	}
	if s.CheckoutInvoiceID == nil {
		s.CheckoutInvoiceID = Ptr(fmt.Sprintf("inv_%d", f.nextID))
	}
	if f.Sessions == nil {
		f.Sessions = map[string]*types.CheckoutSessionResponse{}
	}
	f.Sessions[*s.ID] = s
	return s, nil
}

// PendingSession is a hosted-link session that has not been paid.
func PendingSession() *types.CheckoutSessionResponse {
	expiresAt := time.Now().Add(30 * time.Minute)
	return &types.CheckoutSessionResponse{
		ExpiresAt:      &expiresAt,
		CheckoutStatus: types.CheckoutStatusPending.ToPointer(),
		Terminal:       Ptr(false),
		PaymentAction:  &types.PaymentAction{URL: Ptr("https://pay.example.com/link")},
	}
}

func (f *Payments) CreateCheckoutSession(_ context.Context, req types.CreateCheckoutSessionRequest) (*dtos.CreateCheckoutSessionResponse, error) {
	s, err := f.startSession("subscription", &types.CheckoutParams{PaymentProvider: req.PaymentProvider, PaymentProviderConfig: req.PaymentProviderConfig})
	if err != nil {
		return nil, err
	}
	return &dtos.CreateCheckoutSessionResponse{CheckoutSessionResponse: s}, nil
}

func (f *Payments) GetCheckoutSession(_ context.Context, id string) (*dtos.GetCheckoutSessionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.GetSessionCalls++
	s, ok := f.Sessions[id]
	if !ok {
		return nil, NotFound()
	}
	if f.SessionOnGet != nil {
		return &dtos.GetCheckoutSessionResponse{CheckoutSessionResponse: f.SessionOnGet(s)}, nil
	}
	done := *s
	done.CheckoutStatus = types.CheckoutStatusCompleted.ToPointer()
	done.Terminal = Ptr(true)
	return &dtos.GetCheckoutSessionResponse{CheckoutSessionResponse: &done}, nil
}

func (f *Payments) CancelCheckoutSession(_ context.Context, id string) (*dtos.CancelCheckoutSessionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cancelled = append(f.Cancelled, id)
	s, ok := f.Sessions[id]
	if !ok {
		return nil, NotFound()
	}
	status := f.CancelStatus
	if status == "" {
		status = types.CheckoutStatusExpired
	}
	out := *s
	out.CheckoutStatus = status.ToPointer()
	out.Terminal = Ptr(true)
	return &dtos.CancelCheckoutSessionResponse{CheckoutSessionResponse: &out}, nil
}

func (f *Payments) CreateCheckoutInvoice(_ context.Context, req types.CreateInvoiceRequest) (*dtos.CreateInvoiceResponse, error) {
	s, err := f.startSession("invoice", req.Checkout)
	if err != nil {
		return nil, err
	}
	return &dtos.CreateInvoiceResponse{InvoiceResponse: &types.InvoiceResponse{CheckoutSession: s}}, nil
}

func (f *Payments) ListPayments(_ context.Context, req dtos.ListPaymentsRequest) (*dtos.ListPaymentsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	status := f.PaymentStatus
	if status == "" {
		status = types.PaymentStatusSucceeded
	}
	items := make([]types.PaymentResponse, 0, len(req.PaymentIds))
	for _, id := range req.PaymentIds {
		items = append(items, types.PaymentResponse{ID: Ptr(id), PaymentStatus: &status})
	}
	return &dtos.ListPaymentsResponse{ListPaymentsResponse: &types.ListPaymentsResponse{Items: items}}, nil
}

func (f *Payments) ListSavedMethods(_ context.Context, _, _ string) ([]e2eprobe.SavedPaymentMethod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return append([]e2eprobe.SavedPaymentMethod(nil), f.SavedMethods...), nil
}

func (f *Payments) CreateSetupLink(_ context.Context, _, _, _ string) (string, error) {
	return f.SetupURL, nil
}

func (f *Payments) CreatePortalSession(_ context.Context, _ string) (string, error) {
	return "portal_token", nil
}

func (f *Payments) PortalListSavedMethods(ctx context.Context, _, provider string) ([]e2eprobe.SavedPaymentMethod, error) {
	return f.ListSavedMethods(ctx, "", provider)
}

func (f *Payments) PortalAddMethod(_ context.Context, _, _, _ string) (string, error) {
	return f.PortalAddURL, f.PortalAddErr
}

func (f *Payments) PortalSetDefaultMethod(_ context.Context, _, _, methodID string) ([]e2eprobe.SavedPaymentMethod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Defaults = append(f.Defaults, methodID)
	if !f.IgnoreSetDefault {
		for i := range f.SavedMethods {
			f.SavedMethods[i].IsDefault = f.SavedMethods[i].ID == methodID
		}
	}
	return append([]e2eprobe.SavedPaymentMethod(nil), f.SavedMethods...), nil
}

func (f *Payments) PortalDeleteMethod(_ context.Context, _, _, methodID string) ([]e2eprobe.SavedPaymentMethod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.PortalDeleteErr != nil {
		return nil, f.PortalDeleteErr
	}
	f.Deleted = append(f.Deleted, methodID)
	kept := f.SavedMethods[:0]
	for _, m := range f.SavedMethods {
		if m.ID != methodID {
			kept = append(kept, m)
		}
	}
	f.SavedMethods = kept
	return append([]e2eprobe.SavedPaymentMethod(nil), f.SavedMethods...), nil
}

func (f *Payments) ExecuteSubscriptionModify(_ context.Context, subID string, req types.ExecuteSubscriptionModifyRequest) (*dtos.ExecuteSubscriptionModifyResponse, error) {
	if f.Ungated["modify"] {
		return &dtos.ExecuteSubscriptionModifyResponse{SubscriptionModifyResponse: &types.SubscriptionModifyResponse{}}, nil
	}
	sess, err := f.startSession("modify", req.Checkout)
	if err != nil {
		return nil, err
	}
	if f.OnModify != nil {
		f.OnModify(subID, req)
	}
	return &dtos.ExecuteSubscriptionModifyResponse{SubscriptionModifyResponse: &types.SubscriptionModifyResponse{CheckoutSession: sess}}, nil
}

func (f *Payments) AddSubscriptionAddon(_ context.Context, req types.AddAddonRequest) (*dtos.AddSubscriptionAddonResponse, error) {
	if f.Ungated["addon"] {
		return &dtos.AddSubscriptionAddonResponse{AddAddonToSubscriptionResponse: &types.AddAddonToSubscriptionResponse{
			AddonStatus: types.AddonStatusActive.ToPointer(),
		}}, nil
	}
	sess, err := f.startSession("addon", req.Checkout)
	if err != nil {
		return nil, err
	}
	addonID := req.AddonID
	return &dtos.AddSubscriptionAddonResponse{AddAddonToSubscriptionResponse: &types.AddAddonToSubscriptionResponse{
		ID:              Ptr("assoc_1"),
		AddonID:         &addonID,
		AddonStatus:     types.AddonStatusPending.ToPointer(),
		CheckoutSession: sess,
	}}, nil
}

func (f *Payments) GetSubscriptionAddonAssociations(_ context.Context, _ string) (*dtos.GetSubscriptionAddonAssociationsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &dtos.GetSubscriptionAddonAssociationsResponse{ListAddonAssociationsResponse: &types.ListAddonAssociationsResponse{
		Items: append([]types.AddonAssociationResponse(nil), f.AddonAssociations...),
	}}, nil
}

func (f *Payments) GetAddonByLookupKey(_ context.Context, lookupKey string) (*dtos.GetAddonByLookupKeyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.Addons[lookupKey]
	if !ok {
		return nil, NotFound()
	}
	return &dtos.GetAddonByLookupKeyResponse{AddonResponse: &types.AddonResponse{ID: &id}}, nil
}

func (f *Payments) CreateAddon(_ context.Context, req types.CreateAddonRequest) (*dtos.CreateAddonResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Addons == nil {
		f.Addons = map[string]string{}
	}
	id := fmt.Sprintf("addon_%d", len(f.Addons)+1)
	f.Addons[req.LookupKey] = id
	return &dtos.CreateAddonResponse{CreateAddonResponse: &types.CreateAddonResponse{ID: &id}}, nil
}

func (f *Payments) CreateCreditNote(_ context.Context, req types.CreateCreditNoteRequest) (*dtos.CreateCreditNoteResponse, error) {
	f.mu.Lock()
	f.CreditNotes = append(f.CreditNotes, req)
	hook := f.OnCreditNote
	f.mu.Unlock()
	if hook != nil {
		hook(req)
	}
	return &dtos.CreateCreditNoteResponse{CreditNoteResponse: &types.CreditNoteResponse{ID: Ptr("cn_1")}}, nil
}

func (f *Payments) ListRefunds(_ context.Context, _ dtos.ListRefundsRequest) (*dtos.ListRefundsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &dtos.ListRefundsResponse{ListRefundsResponse: &types.ListRefundsResponse{
		Items: append([]types.RefundResponse(nil), f.Refunds...),
	}}, nil
}
