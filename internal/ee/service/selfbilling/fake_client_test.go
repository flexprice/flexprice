package selfbilling

import (
	"context"
	"sync"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

type fakeClient struct {
	mu             sync.Mutex
	customers      map[string]bool
	legacyExtIDs   map[string]string // extID -> legacyExtID passed to EnsureCustomer
	subs           map[string]*remoteSubscription
	checkouts      map[string]*remoteCheckout
	ingested       [][]usageEvent
	trialsStarted  int
	trialCheckouts int
	err            error
}

func newFakeClient() *fakeClient {
	return &fakeClient{customers: map[string]bool{}, legacyExtIDs: map[string]string{}, subs: map[string]*remoteSubscription{}, checkouts: map[string]*remoteCheckout{}}
}

func (f *fakeClient) EnsureCustomer(_ context.Context, in customerInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.customers[in.extID] = true
	f.legacyExtIDs[in.extID] = in.legacyExtID
	return f.err
}

func (f *fakeClient) StartTrial(_ context.Context, ext string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trialsStarted++
	f.subs[ext] = &remoteSubscription{id: "sub_" + ext, status: "trialing"}
	return f.err
}

func (f *fakeClient) LatestSubscription(_ context.Context, ext string) (*remoteSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subs[ext], f.err
}

func (f *fakeClient) UpdateBillingDetails(context.Context, string, billingDetails) error {
	return f.err
}

func (f *fakeClient) StartTrialEndCheckout(_ context.Context, in checkoutInput) (*remoteCheckout, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trialCheckouts++
	co := &remoteCheckout{id: "cs_" + in.subscriptionID, status: "pending", url: "https://checkout.stripe.test/" + in.subscriptionID, extID: in.extID}
	f.checkouts[co.id] = co
	return co, f.err
}

func (f *fakeClient) GetCheckout(_ context.Context, id string) (*remoteCheckout, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	co, ok := f.checkouts[id]
	if !ok {
		return nil, ierr.NewErrorf("checkout session %s not found", id).Mark(ierr.ErrNotFound)
	}
	return co, f.err
}

func (f *fakeClient) CancelCheckout(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if co, ok := f.checkouts[id]; ok {
		co.status, co.terminal = "failed", true
	}
	return f.err
}

func (f *fakeClient) PortalSession(context.Context, string) (*portalSession, error) {
	return &portalSession{url: "https://portal.test"}, f.err
}

func (f *fakeClient) Summary(_ context.Context, ext string) (*remoteSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &remoteSummary{subscription: f.subs[ext]}, f.err
}

func (f *fakeClient) IngestBulk(_ context.Context, events []usageEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ingested = append(f.ingested, events)
	return f.err
}
