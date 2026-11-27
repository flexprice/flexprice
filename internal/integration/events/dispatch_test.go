package events

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

type cnDispatchCreditNoteRepo struct {
	creditnote.Repository
	cn    *creditnote.CreditNote
	err   error
	calls int
}

func (f *cnDispatchCreditNoteRepo) Get(_ context.Context, _ string) (*creditnote.CreditNote, error) {
	f.calls++
	return f.cn, f.err
}

type cnDispatchRefundRepo struct {
	refund.Repository
	refunds   []*refund.Refund
	getErr    error
	listErr   error
	getCalls  int
	listCalls int
	listed    *types.RefundFilter
}

func (f *cnDispatchRefundRepo) Get(_ context.Context, id string) (*refund.Refund, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, r := range f.refunds {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, ierr.NewError("refund not found").Mark(ierr.ErrNotFound)
}

func (f *cnDispatchRefundRepo) List(_ context.Context, filter *types.RefundFilter) ([]*refund.Refund, error) {
	f.listCalls++
	f.listed = filter
	return f.refunds, f.listErr
}

// cnDispatchConnectionRepo returns the Zoho Books connection, or not found when conn is nil or
// owned by a billing entity other than the one in ctx, and records the billing entity at lookup time.
type cnDispatchConnectionRepo struct {
	connection.Repository
	conn            *connection.Connection
	owner           string
	err             error
	calls           int
	billingEntityID string
}

func (f *cnDispatchConnectionRepo) GetByProvider(ctx context.Context, _ types.SecretProvider) (*connection.Connection, error) {
	f.calls++
	f.billingEntityID = types.GetBillingEntityID(ctx)
	if f.err != nil {
		return nil, f.err
	}
	if f.conn == nil || (f.owner != "" && f.owner != f.billingEntityID) {
		return nil, ierr.NewError("connection not found").Mark(ierr.ErrNotFound)
	}
	return f.conn, nil
}

func zohoConnection(invoiceOutbound, creditNoteOutbound bool) *connection.Connection {
	return &connection.Connection{SyncConfig: &types.SyncConfig{
		Invoice:    &types.EntitySyncConfig{Outbound: invoiceOutbound},
		CreditNote: &types.EntitySyncConfig{Outbound: creditNoteOutbound},
	}}
}

// cnDispatchEntityScope scopes every invoice to the billing entity "be_<invoice id>".
type cnDispatchEntityScope struct {
	interfaces.BillingEntityScope
	err        error
	invoiceIDs []string
}

func (f *cnDispatchEntityScope) ScopeToInvoice(ctx context.Context, invoiceID string) (context.Context, error) {
	f.invoiceIDs = append(f.invoiceIDs, invoiceID)
	if f.err != nil {
		return nil, f.err
	}
	return types.SetBillingEntityID(ctx, "be_"+invoiceID), nil
}

func integrationEventsConfig(enabled bool) *config.Configuration {
	return &config.Configuration{IntegrationEvents: config.IntegrationEventsConfig{Enabled: enabled}}
}

func cnDispatchEvent(payload string) *types.WebhookEvent {
	return &types.WebhookEvent{TenantID: "ten_1", EnvironmentID: "env_1", Payload: json.RawMessage(payload)}
}

// Tests run without a global Temporal service, so reaching the workflow start returns
// errTemporalUnavailable.
func TestDispatchCreditNoteVendorSync(t *testing.T) {
	finalizedAdjustment := func() *creditnote.CreditNote {
		return &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeAdjustment, CreditNoteStatus: types.CreditNoteStatusFinalized}
	}
	dbDown := errors.New("db down")

	tests := []struct {
		name          string
		cfg           *config.Configuration
		payload       string
		cn            *creditnote.CreditNote
		cnErr         error
		conn          *connection.Connection
		connErr       error
		scopeErr      error
		wantErr       error
		wantStart     bool
		wantCNRead    bool
		wantConnCheck bool
	}{
		{name: "finalized adjustment credit note starts the sync", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`, cn: finalizedAdjustment(), conn: zohoConnection(true, true), wantStart: true, wantCNRead: true, wantConnCheck: true},
		{name: "nil config still dispatches", payload: `{"credit_note_id":"cn_1"}`, cn: finalizedAdjustment(), conn: zohoConnection(true, true), wantStart: true, wantCNRead: true, wantConnCheck: true},
		{name: "integration events disabled reads nothing", cfg: integrationEventsConfig(false), payload: `{"credit_note_id":"cn_1"}`, cn: finalizedAdjustment(), conn: zohoConnection(true, true)},
		{name: "malformed payload is dropped", cfg: integrationEventsConfig(true), payload: `{`, cn: finalizedAdjustment()},
		{name: "payload without credit note id is dropped", cfg: integrationEventsConfig(true), payload: `{}`, cn: finalizedAdjustment()},
		{name: "credit note read error is retried", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`, cnErr: dbDown, wantErr: dbDown, wantCNRead: true},
		{name: "draft adjustment credit note is skipped", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`,
			cn: &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeAdjustment, CreditNoteStatus: types.CreditNoteStatusDraft}, conn: zohoConnection(true, true), wantCNRead: true},
		{name: "voided adjustment credit note starts the sync", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`,
			cn:   &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeAdjustment, CreditNoteStatus: types.CreditNoteStatusVoided},
			conn: zohoConnection(true, true), wantStart: true, wantCNRead: true, wantConnCheck: true},
		{name: "voided refund credit note starts the sync", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`,
			cn:   &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeRefund, CreditNoteStatus: types.CreditNoteStatusVoided},
			conn: zohoConnection(true, true), wantStart: true, wantCNRead: true, wantConnCheck: true},
		{name: "voided credit note with credit note sync off is skipped", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`,
			cn:   &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeAdjustment, CreditNoteStatus: types.CreditNoteStatusVoided},
			conn: zohoConnection(true, false), wantCNRead: true, wantConnCheck: true},
		{name: "refund credit note waits for refund.succeeded", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`,
			cn: &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeRefund, CreditNoteStatus: types.CreditNoteStatusFinalized}, conn: zohoConnection(true, true), wantCNRead: true},
		{name: "no Zoho Books connection is skipped", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`, cn: finalizedAdjustment(), wantCNRead: true, wantConnCheck: true},
		{name: "credit note sync off is skipped", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`, cn: finalizedAdjustment(), conn: zohoConnection(true, false), wantCNRead: true, wantConnCheck: true},
		{name: "invoice sync off is skipped", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`, cn: finalizedAdjustment(), conn: zohoConnection(false, true), wantCNRead: true, wantConnCheck: true},
		{name: "connection without sync config is skipped", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`, cn: finalizedAdjustment(), conn: &connection.Connection{}, wantCNRead: true, wantConnCheck: true},
		{name: "connection lookup error is retried", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`, cn: finalizedAdjustment(), connErr: dbDown, wantErr: dbDown, wantCNRead: true, wantConnCheck: true},
		{name: "billing entity scope error is retried", cfg: integrationEventsConfig(true), payload: `{"credit_note_id":"cn_1"}`, cn: finalizedAdjustment(), conn: zohoConnection(true, true), scopeErr: dbDown, wantErr: dbDown, wantCNRead: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cnRepo := &cnDispatchCreditNoteRepo{cn: tt.cn, err: tt.cnErr}
			connRepo := &cnDispatchConnectionRepo{conn: tt.conn, err: tt.connErr}
			scope := &cnDispatchEntityScope{err: tt.scopeErr}

			err := DispatchCreditNoteVendorSync(context.Background(), tt.cfg, connRepo, &cnPushMappingRepo{}, cnRepo, scope, testLogger(), cnDispatchEvent(tt.payload), "msg_1")

			switch {
			case tt.wantStart:
				require.ErrorIs(t, err, errTemporalUnavailable)
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			default:
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantCNRead, cnRepo.calls > 0, "credit note read")
			require.Equal(t, tt.wantConnCheck, connRepo.calls > 0, "connection lookup")
			if tt.wantConnCheck {
				require.Equal(t, []string{"inv_1"}, scope.invoiceIDs)
				require.Equal(t, "be_inv_1", connRepo.billingEntityID, "connection lookup must run in the invoice's billing entity")
			}
		})
	}
}

func TestDispatchCreditNoteVendorSync_WithoutEntityScope(t *testing.T) {
	cnRepo := &cnDispatchCreditNoteRepo{cn: &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeAdjustment, CreditNoteStatus: types.CreditNoteStatusFinalized}}
	connRepo := &cnDispatchConnectionRepo{conn: zohoConnection(true, true)}

	err := DispatchCreditNoteVendorSync(context.Background(), integrationEventsConfig(true), connRepo, &cnPushMappingRepo{}, cnRepo, nil, testLogger(), cnDispatchEvent(`{"credit_note_id":"cn_1"}`), "msg_1")

	require.ErrorIs(t, err, errTemporalUnavailable)
	require.Empty(t, connRepo.billingEntityID)
}

func TestDispatchCreditNoteVendorSync_AlreadySyncedIsSkipped(t *testing.T) {
	cnRepo := &cnDispatchCreditNoteRepo{cn: &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeAdjustment, CreditNoteStatus: types.CreditNoteStatusFinalized}}
	connRepo := &cnDispatchConnectionRepo{conn: zohoConnection(true, true)}
	mappingRepo := &cnPushMappingRepo{mappings: []*entityintegrationmapping.EntityIntegrationMapping{{EntityID: "cn_1", ProviderEntityID: "zcn_1"}}}

	err := DispatchCreditNoteVendorSync(context.Background(), integrationEventsConfig(true), connRepo, mappingRepo, cnRepo, &cnDispatchEntityScope{}, testLogger(), cnDispatchEvent(`{"credit_note_id":"cn_1"}`), "msg_1")

	require.NoError(t, err, "a redelivered event for a synced credit note starts nothing")
	require.Equal(t, 1, mappingRepo.calls)
}

func TestDispatchRefundVendorSync(t *testing.T) {
	refundCreditNote := &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeRefund, CreditNoteStatus: types.CreditNoteStatusFinalized}
	adjustmentCreditNote := &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeAdjustment, CreditNoteStatus: types.CreditNoteStatusFinalized}
	refundRow := func(id string, status types.RefundStatus) *refund.Refund {
		return &refund.Refund{ID: id, InvoiceID: "inv_1", CreditNoteID: lo.ToPtr("cn_1"), RefundStatus: status}
	}
	settled := refundRow("ref_1", types.RefundStatusSucceeded)
	dbDown := errors.New("db down")

	tests := []struct {
		name          string
		cfg           *config.Configuration
		payload       string
		refunds       []*refund.Refund
		refundGetErr  error
		refundListErr error
		cn            *creditnote.CreditNote
		cnErr         error
		conn          *connection.Connection
		connErr       error
		wantErr       error
		wantStart     bool
		wantConnCheck bool
		wantCNRead    bool
		wantList      bool
	}{
		{name: "single refund settled starts the sync", payload: `{"refund_id":"ref_1"}`, refunds: []*refund.Refund{settled}, cn: refundCreditNote, conn: zohoConnection(true, true), wantStart: true, wantConnCheck: true, wantCNRead: true, wantList: true},
		{name: "last of several refunds settled starts the sync", payload: `{"refund_id":"ref_1"}`,
			refunds: []*refund.Refund{settled, refundRow("ref_2", types.RefundStatusSucceeded)}, cn: refundCreditNote, conn: zohoConnection(true, true), wantStart: true, wantConnCheck: true, wantCNRead: true, wantList: true},
		{name: "failed gateway refund with settled wallet fallback starts the sync", payload: `{"refund_id":"ref_1"}`,
			refunds: []*refund.Refund{settled, refundRow("ref_2", types.RefundStatusFailed)}, cn: refundCreditNote, conn: zohoConnection(true, true), wantStart: true, wantConnCheck: true, wantCNRead: true, wantList: true},
		{name: "cancelled refund counts as finished", payload: `{"refund_id":"ref_1"}`,
			refunds: []*refund.Refund{settled, refundRow("ref_2", types.RefundStatusCancelled)}, cn: refundCreditNote, conn: zohoConnection(true, true), wantStart: true, wantConnCheck: true, wantCNRead: true, wantList: true},
		{name: "processing refund defers the sync", payload: `{"refund_id":"ref_1"}`,
			refunds: []*refund.Refund{settled, refundRow("ref_2", types.RefundStatusProcessing)}, cn: refundCreditNote, conn: zohoConnection(true, true), wantConnCheck: true, wantCNRead: true, wantList: true},
		{name: "pending refund defers the sync", payload: `{"refund_id":"ref_1"}`,
			refunds: []*refund.Refund{settled, refundRow("ref_2", types.RefundStatusPending)}, cn: refundCreditNote, conn: zohoConnection(true, true), wantConnCheck: true, wantCNRead: true, wantList: true},
		{name: "integration events disabled reads nothing", cfg: integrationEventsConfig(false), payload: `{"refund_id":"ref_1"}`, refunds: []*refund.Refund{settled}, cn: refundCreditNote, conn: zohoConnection(true, true)},
		{name: "malformed payload is dropped", payload: `{`, refunds: []*refund.Refund{settled}},
		{name: "payload without refund id is dropped", payload: `{}`, refunds: []*refund.Refund{settled}},
		{name: "refund read error is retried", payload: `{"refund_id":"ref_1"}`, refundGetErr: dbDown, wantErr: dbDown},
		{name: "refund without a credit note is skipped before the connection lookup", payload: `{"refund_id":"ref_1"}`,
			refunds: []*refund.Refund{{ID: "ref_1", InvoiceID: "inv_1", RefundStatus: types.RefundStatusSucceeded}}, conn: zohoConnection(true, true)},
		{name: "no Zoho Books connection skips the credit note read and refund list", payload: `{"refund_id":"ref_1"}`, refunds: []*refund.Refund{settled}, cn: refundCreditNote, wantConnCheck: true},
		{name: "credit note sync off skips the credit note read and refund list", payload: `{"refund_id":"ref_1"}`, refunds: []*refund.Refund{settled}, cn: refundCreditNote, conn: zohoConnection(true, false), wantConnCheck: true},
		{name: "invoice sync off skips the credit note read and refund list", payload: `{"refund_id":"ref_1"}`, refunds: []*refund.Refund{settled}, cn: refundCreditNote, conn: zohoConnection(false, true), wantConnCheck: true},
		{name: "connection lookup error is retried", payload: `{"refund_id":"ref_1"}`, refunds: []*refund.Refund{settled}, cn: refundCreditNote, connErr: dbDown, wantErr: dbDown, wantConnCheck: true},
		{name: "credit note read error is retried", payload: `{"refund_id":"ref_1"}`, refunds: []*refund.Refund{settled}, cnErr: dbDown, conn: zohoConnection(true, true), wantErr: dbDown, wantConnCheck: true, wantCNRead: true},
		{name: "adjustment credit note is skipped", payload: `{"refund_id":"ref_1"}`, refunds: []*refund.Refund{settled}, cn: adjustmentCreditNote, conn: zohoConnection(true, true), wantConnCheck: true, wantCNRead: true},
		{name: "refund list error is retried", payload: `{"refund_id":"ref_1"}`, refunds: []*refund.Refund{settled}, refundListErr: dbDown, cn: refundCreditNote, conn: zohoConnection(true, true), wantErr: dbDown, wantConnCheck: true, wantCNRead: true, wantList: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			if cfg == nil {
				cfg = integrationEventsConfig(true)
			}
			refundRepo := &cnDispatchRefundRepo{refunds: tt.refunds, getErr: tt.refundGetErr, listErr: tt.refundListErr}
			cnRepo := &cnDispatchCreditNoteRepo{cn: tt.cn, err: tt.cnErr}
			connRepo := &cnDispatchConnectionRepo{conn: tt.conn, err: tt.connErr}
			scope := &cnDispatchEntityScope{}

			err := DispatchRefundVendorSync(context.Background(), cfg, connRepo, &cnPushMappingRepo{}, cnRepo, refundRepo, scope, testLogger(), cnDispatchEvent(tt.payload), "msg_1")

			switch {
			case tt.wantStart:
				require.ErrorIs(t, err, errTemporalUnavailable)
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			default:
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantConnCheck, connRepo.calls > 0, "connection lookup")
			require.Equal(t, tt.wantCNRead, cnRepo.calls > 0, "credit note read")
			require.Equal(t, tt.wantList, refundRepo.listCalls > 0, "refund list")
			if tt.wantConnCheck {
				require.Equal(t, []string{"inv_1"}, scope.invoiceIDs)
				require.Equal(t, "be_inv_1", connRepo.billingEntityID, "connection lookup must run in the refund invoice's billing entity")
			}
			if tt.wantList {
				require.Equal(t, []string{"cn_1"}, refundRepo.listed.CreditNoteIDs)
			}
		})
	}
}

func TestDispatchRefundVendorSync_BillingEntityScopeError(t *testing.T) {
	scopeErr := errors.New("invoice not found")
	refundRepo := &cnDispatchRefundRepo{refunds: []*refund.Refund{{ID: "ref_1", InvoiceID: "inv_1", CreditNoteID: lo.ToPtr("cn_1"), RefundStatus: types.RefundStatusSucceeded}}}
	cnRepo := &cnDispatchCreditNoteRepo{}
	connRepo := &cnDispatchConnectionRepo{conn: zohoConnection(true, true)}

	err := DispatchRefundVendorSync(context.Background(), integrationEventsConfig(true), connRepo, &cnPushMappingRepo{}, cnRepo, refundRepo, &cnDispatchEntityScope{err: scopeErr}, testLogger(), cnDispatchEvent(`{"refund_id":"ref_1"}`), "msg_1")

	require.ErrorIs(t, err, scopeErr)
	require.Zero(t, connRepo.calls)
	require.Zero(t, cnRepo.calls)
	require.Zero(t, refundRepo.listCalls)
}

func TestStartZohoBooksCreditNoteSync(t *testing.T) {
	cn := &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1"}
	event := &types.WebhookEvent{TenantID: "ten_1", EnvironmentID: "env_1"}

	t.Run("starts the workflow with the credit note", func(t *testing.T) {
		temporalSvc := &fakeTemporalService{}

		require.NoError(t, startZohoBooksCreditNoteSync(context.Background(), temporalSvc, testLogger(), cn, event))

		require.Len(t, temporalSvc.inputs, 1)
		input, ok := temporalSvc.inputs[0].(*temporalmodels.ZohoBooksCreditNoteSyncWorkflowInput)
		require.True(t, ok, "unexpected workflow input %#v", temporalSvc.inputs[0])
		require.Equal(t, "cn_1", input.CreditNoteID)
		require.Equal(t, "ten_1", input.TenantID)
		require.Equal(t, "env_1", input.EnvironmentID)
	})

	t.Run("a sync already running is returned so the event is retried", func(t *testing.T) {
		temporalSvc := &fakeTemporalService{err: ierr.NewError("already started").Mark(ierr.ErrAlreadyExists)}

		err := startZohoBooksCreditNoteSync(context.Background(), temporalSvc, testLogger(), cn, event)

		require.True(t, ierr.IsAlreadyExists(err))
	})

	t.Run("a start failure is returned", func(t *testing.T) {
		temporalSvc := &fakeTemporalService{err: ierr.NewError("temporal down").Mark(ierr.ErrInternal)}

		require.Error(t, startZohoBooksCreditNoteSync(context.Background(), temporalSvc, testLogger(), cn, event))
	})
}

type cnPushMappingRepo struct {
	entityintegrationmapping.Repository
	mappings []*entityintegrationmapping.EntityIntegrationMapping
	err      error
	calls    int
	listed   *types.EntityIntegrationMappingFilter
}

func (f *cnPushMappingRepo) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	f.calls++
	f.listed = filter
	return f.mappings, f.err
}

func TestPushCreditNoteVendorSync(t *testing.T) {
	dbDown := errors.New("db down")
	adjustment := func(status types.CreditNoteStatus) *creditnote.CreditNote {
		return &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeAdjustment, CreditNoteStatus: status}
	}
	refundNote := &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeRefund, CreditNoteStatus: types.CreditNoteStatusFinalized}
	synced := []*entityintegrationmapping.EntityIntegrationMapping{{EntityID: "cn_1", ProviderEntityID: "zcn_1"}}
	refundRow := func(id string, status types.RefundStatus) *refund.Refund {
		return &refund.Refund{ID: id, CreditNoteID: lo.ToPtr("cn_1"), RefundStatus: status}
	}

	tests := []struct {
		name           string
		cn             *creditnote.CreditNote
		conn           *connection.Connection
		connErr        error
		mappings       []*entityintegrationmapping.EntityIntegrationMapping
		mappingErr     error
		refunds        []*refund.Refund
		refundErr      error
		opts           []DispatchOption
		wantErr        error
		wantValidation bool
		wantConnCheck  bool
		wantMapping    bool
		wantRefunds    bool
	}{
		{name: "finalized adjustment not yet synced starts the sync", cn: adjustment(types.CreditNoteStatusFinalized), conn: zohoConnection(true, true), wantErr: errTemporalUnavailable, wantConnCheck: true, wantMapping: true},
		{name: "voided credit note starts the sync even when synced", cn: adjustment(types.CreditNoteStatusVoided), conn: zohoConnection(true, true), mappings: synced, wantErr: errTemporalUnavailable, wantConnCheck: true},
		{name: "draft credit note is rejected before any lookup", cn: adjustment(types.CreditNoteStatusDraft), conn: zohoConnection(true, true), wantValidation: true},
		{name: "no Zoho Books connection is rejected", cn: adjustment(types.CreditNoteStatusFinalized), wantValidation: true, wantConnCheck: true},
		{name: "credit note sync off is rejected", cn: adjustment(types.CreditNoteStatusFinalized), conn: zohoConnection(true, false), wantValidation: true, wantConnCheck: true},
		{name: "invoice sync off is rejected", cn: adjustment(types.CreditNoteStatusFinalized), conn: zohoConnection(false, true), wantValidation: true, wantConnCheck: true},
		{name: "connection lookup error is returned", cn: adjustment(types.CreditNoteStatusFinalized), connErr: dbDown, wantErr: dbDown, wantConnCheck: true},
		{name: "credit note already synced is rejected", cn: adjustment(types.CreditNoteStatusFinalized), conn: zohoConnection(true, true), mappings: synced, wantValidation: true, wantConnCheck: true, wantMapping: true},
		{name: "mapping lookup error is returned", cn: adjustment(types.CreditNoteStatusFinalized), conn: zohoConnection(true, true), mappingErr: dbDown, wantErr: dbDown, wantConnCheck: true, wantMapping: true},
		{name: "refund credit note with finished refunds starts the sync", cn: refundNote, conn: zohoConnection(true, true),
			refunds: []*refund.Refund{refundRow("ref_1", types.RefundStatusSucceeded), refundRow("ref_2", types.RefundStatusFailed)}, wantErr: errTemporalUnavailable, wantConnCheck: true, wantMapping: true, wantRefunds: true},
		{name: "refund credit note with a refund in progress is rejected", cn: refundNote, conn: zohoConnection(true, true),
			refunds: []*refund.Refund{refundRow("ref_1", types.RefundStatusSucceeded), refundRow("ref_2", types.RefundStatusProcessing)}, wantValidation: true, wantConnCheck: true, wantRefunds: true},
		{name: "refund list error is returned", cn: refundNote, conn: zohoConnection(true, true), refundErr: dbDown, wantErr: dbDown, wantConnCheck: true, wantRefunds: true},
		{name: "push limited to Zoho Books starts the sync", cn: adjustment(types.CreditNoteStatusFinalized), conn: zohoConnection(true, true),
			opts: []DispatchOption{OnlyProvider(types.SecretProviderZohoBooks)}, wantErr: errTemporalUnavailable, wantConnCheck: true, wantMapping: true},
		{name: "push limited to a provider without credit note sync is rejected before any lookup", cn: adjustment(types.CreditNoteStatusFinalized), conn: zohoConnection(true, true),
			opts: []DispatchOption{OnlyProvider(types.SecretProviderStripe)}, wantValidation: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connRepo := &cnDispatchConnectionRepo{conn: tt.conn, err: tt.connErr}
			mappingRepo := &cnPushMappingRepo{mappings: tt.mappings, err: tt.mappingErr}
			refundRepo := &cnDispatchRefundRepo{refunds: tt.refunds, listErr: tt.refundErr}

			err := PushCreditNoteVendorSync(context.Background(), connRepo, mappingRepo, refundRepo, &cnDispatchEntityScope{}, testLogger(), tt.cn, cnDispatchEvent(`{}`), tt.opts...)

			switch {
			case tt.wantValidation:
				require.True(t, ierr.IsValidation(err), "want a validation error, got %v", err)
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			default:
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantConnCheck, connRepo.calls > 0, "connection lookup")
			require.Equal(t, tt.wantMapping, mappingRepo.calls > 0, "mapping lookup")
			require.Equal(t, tt.wantRefunds, refundRepo.listCalls > 0, "refund list")
			if tt.wantMapping {
				require.Equal(t, types.IntegrationEntityTypeCreditNote, mappingRepo.listed.EntityType)
				require.Equal(t, "cn_1", mappingRepo.listed.EntityID)
			}
			if tt.wantRefunds {
				require.Equal(t, []string{"cn_1"}, refundRepo.listed.CreditNoteIDs)
			}
		})
	}
}

func TestPushCreditNoteVendorSync_OwnedConnection(t *testing.T) {
	finalized := &creditnote.CreditNote{ID: "cn_1", InvoiceID: "inv_1", CreditNoteType: types.CreditNoteTypeAdjustment, CreditNoteStatus: types.CreditNoteStatusFinalized}

	t.Run("the invoice's billing entity owns the connection", func(t *testing.T) {
		connRepo := &cnDispatchConnectionRepo{conn: zohoConnection(true, true), owner: "be_inv_1"}
		scope := &cnDispatchEntityScope{}

		err := PushCreditNoteVendorSync(context.Background(), connRepo, &cnPushMappingRepo{}, &cnDispatchRefundRepo{}, scope, testLogger(), finalized, cnDispatchEvent(`{}`), Manual())

		require.ErrorIs(t, err, errTemporalUnavailable)
		require.Equal(t, []string{"inv_1"}, scope.invoiceIDs)
		require.Equal(t, "be_inv_1", connRepo.billingEntityID, "the connection lookup runs in the invoice's billing entity")
	})

	t.Run("another billing entity owns the only connection", func(t *testing.T) {
		connRepo := &cnDispatchConnectionRepo{conn: zohoConnection(true, true), owner: "be_other"}

		err := PushCreditNoteVendorSync(context.Background(), connRepo, &cnPushMappingRepo{}, &cnDispatchRefundRepo{}, &cnDispatchEntityScope{}, testLogger(), finalized, cnDispatchEvent(`{}`), Manual())

		require.True(t, ierr.IsValidation(err), "want a validation error, got %v", err)
	})

	t.Run("a billing entity scope error is returned before the connection lookup", func(t *testing.T) {
		scopeErr := errors.New("invoice not found")
		connRepo := &cnDispatchConnectionRepo{conn: zohoConnection(true, true)}

		err := PushCreditNoteVendorSync(context.Background(), connRepo, &cnPushMappingRepo{}, &cnDispatchRefundRepo{}, &cnDispatchEntityScope{err: scopeErr}, testLogger(), finalized, cnDispatchEvent(`{}`))

		require.ErrorIs(t, err, scopeErr)
		require.Zero(t, connRepo.calls)
	})
}
