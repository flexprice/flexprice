package service

import (
	"errors"
	"testing"
	"time"

	cerrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/email"
	"github.com/flexprice/flexprice/internal/email/local"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestTakeTurns(t *testing.T) {
	queue := func(ids ...string) []*emaillog.EmailLog {
		return lo.Map(ids, func(id string, _ int) *emaillog.EmailLog { return &emaillog.EmailLog{ID: id} })
	}
	ids := func(es []*emaillog.EmailLog) []string {
		return lo.Map(es, func(e *emaillog.EmailLog, _ int) string { return e.ID })
	}
	big, small := queue("a1", "a2", "a3", "a4"), queue("b1")

	assert.Equal(t, []string{"a1", "b1", "a2", "a3", "a4"}, ids(takeTurns([][]*emaillog.EmailLog{big, small}, 10)),
		"a large backlog does not hold up another environment")
	assert.Equal(t, []string{"a1", "b1", "a2"}, ids(takeTurns([][]*emaillog.EmailLog{big, small}, 3)))
	assert.Empty(t, takeTurns(nil, 10))
}

func TestClassifyDelivery(t *testing.T) {
	pdfNotReady := cerrors.Mark(ierr.NewError("pdf").Mark(ierr.ErrServiceUnavailable), errAttachmentNotReady)

	for name, tc := range map[string]struct {
		err  error
		want deliveryOutcome
	}{
		"sent":               {nil, deliverySent},
		"rejected":           {ierr.NewError("bad address").Mark(ierr.ErrValidation), deliveryFailed},
		"attachment pending": {pdfNotReady, deliveryDeferred},
		"rate limited":       {ierr.NewError("429").Mark(ierr.ErrTooManyRequests), deliveryRateLimited},
		"provider down":      {ierr.NewError("503").Mark(ierr.ErrServiceUnavailable), deliveryProviderDown},
		"other":              {errors.New("db timeout"), deliveryDeferred},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyDelivery(tc.err))
		})
	}
}

func TestApplyDeliveryEvent(t *testing.T) {
	at := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	logWith := func(status types.EmailDeliveryStatus, messageID *string) *emaillog.EmailLog {
		return &emaillog.EmailLog{ID: "email_1", DeliveryStatus: status, ProviderMessageID: messageID, Metadata: types.Metadata{}}
	}
	event := func(status types.EmailDeliveryStatus, messageID string) email.DeliveryEvent {
		return email.DeliveryEvent{ProviderMessageID: messageID, Status: status, OccurredAt: at}
	}

	t.Run("delivered moves forward and records the time", func(t *testing.T) {
		got := applyDeliveryEvent(logWith(types.EmailDeliveryStatusSent, lo.ToPtr("msg_1")), event(types.EmailDeliveryStatusDelivered, "msg_1"))
		require.NotNil(t, got)
		assert.Equal(t, types.EmailDeliveryStatusDelivered, got.DeliveryStatus)
		assert.Equal(t, at, lo.FromPtr(got.DeliveredAt))
	})

	t.Run("an event before the send is recorded keeps the message id", func(t *testing.T) {
		got := applyDeliveryEvent(logWith(types.EmailDeliveryStatusQueued, nil), event(types.EmailDeliveryStatusSent, "msg_1"))
		require.NotNil(t, got)
		assert.Equal(t, "msg_1", lo.FromPtr(got.ProviderMessageID))
		assert.Equal(t, at, lo.FromPtr(got.SentAt))
	})

	t.Run("bounce keeps the provider reason", func(t *testing.T) {
		ev := event(types.EmailDeliveryStatusBounced, "msg_1")
		ev.Reason = "550 mailbox unavailable"
		got := applyDeliveryEvent(logWith(types.EmailDeliveryStatusSent, lo.ToPtr("msg_1")), ev)
		require.NotNil(t, got)
		assert.Equal(t, "550 mailbox unavailable", lo.FromPtr(got.StatusReason))
	})

	t.Run("a stale event changes nothing", func(t *testing.T) {
		assert.Nil(t, applyDeliveryEvent(logWith(types.EmailDeliveryStatusDelivered, lo.ToPtr("msg_1")), event(types.EmailDeliveryStatusSent, "msg_1")))
	})

	t.Run("an event for another message changes nothing", func(t *testing.T) {
		assert.Nil(t, applyDeliveryEvent(logWith(types.EmailDeliveryStatusSent, lo.ToPtr("msg_1")), event(types.EmailDeliveryStatusDelivered, "msg_2")))
	})
}

type AutoInvoiceEmailSuite struct {
	testutil.BaseServiceTestSuite
	svc EmailNotificationService
}

func TestAutoInvoiceEmail(t *testing.T) {
	suite.Run(t, new(AutoInvoiceEmailSuite))
}

func (s *AutoInvoiceEmailSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	ctx := s.GetContext()
	stores := s.GetStores()
	cfg := s.GetConfig()
	cfg.Email.Enabled = true
	cfg.Email.Provider = string(types.EmailProviderLocal)

	s.svc = NewEmailNotificationService(ServiceParams{
		Logger:        s.GetLogger(),
		Config:        cfg,
		DB:            s.GetDB(),
		TenantRepo:    stores.TenantRepo,
		CustomerRepo:  stores.CustomerRepo,
		InvoiceRepo:   stores.InvoiceRepo,
		SettingsRepo:  stores.SettingsRepo,
		EmailLogRepo:  stores.EmailLogRepo,
		EmailProvider: local.New(s.GetLogger()),
	})

	s.Require().NoError(stores.TenantRepo.Create(ctx, &tenant.Tenant{
		ID: types.GetTenantID(ctx), Name: "Acme", BillingDetails: tenant.TenantBillingDetails{Email: "billing@acme.test"},
	}))
}

func (s *AutoInvoiceEmailSuite) enableAutoSend() {
	ctx := s.GetContext()
	s.Require().NoError(s.GetStores().SettingsRepo.Create(ctx, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("setting"),
		Key:           types.SettingKeyEmailNotificationConfig,
		Value:         map[string]interface{}{"enabled": true, "auto_send": map[string]interface{}{"enabled": true, "entities": []string{"invoice"}}},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
}

func (s *AutoInvoiceEmailSuite) finalizedInvoice(customerEmail string, total int64) *invoice.Invoice {
	ctx := s.GetContext()
	cust := &customer.Customer{ID: types.GenerateUUIDWithPrefix("cust"), Email: customerEmail, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	inv := &invoice.Invoice{
		ID: types.GenerateUUIDWithPrefix("inv"), CustomerID: cust.ID, InvoiceStatus: types.InvoiceStatusFinalized,
		Total: decimal.NewFromInt(total), Currency: "usd", EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.Create(ctx, inv))
	return inv
}

func (s *AutoInvoiceEmailSuite) emailsFor(invoiceID string) []*emaillog.EmailLog {
	filter := types.NewEmailLogFilter()
	filter.EntityIDs = []string{invoiceID}
	logs, err := s.GetStores().EmailLogRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return logs
}

func (s *AutoInvoiceEmailSuite) TestSkipsWhenAutoSendIsOff() {
	inv := s.finalizedInvoice("ap@customer.test", 100)
	s.Require().NoError(s.svc.SendAutoInvoiceEmail(s.GetContext(), inv.ID))
	s.Empty(s.emailsFor(inv.ID))
}

func (s *AutoInvoiceEmailSuite) TestRecordsSkipReasons() {
	s.enableAutoSend()
	tests := []struct {
		name   string
		email  string
		total  int64
		reason string
	}{
		{name: "customer has no email", total: 100, reason: "Customer has no email address"},
		{name: "zero total", email: "ap@customer.test", reason: "Invoice total is zero"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			inv := s.finalizedInvoice(tt.email, tt.total)
			s.Require().NoError(s.svc.SendAutoInvoiceEmail(s.GetContext(), inv.ID))
			s.Require().NoError(s.svc.SendAutoInvoiceEmail(s.GetContext(), inv.ID), "a redelivered event is a no-op")

			logs := s.emailsFor(inv.ID)
			s.Require().Len(logs, 1)
			s.Equal(types.EmailDeliveryStatusSkipped, logs[0].DeliveryStatus)
			s.Equal(types.EmailTriggerAuto, logs[0].Trigger)
			s.Equal(tt.reason, lo.FromPtr(logs[0].StatusReason))
		})
	}
}
