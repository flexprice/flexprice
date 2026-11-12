package service

import (
	"errors"
	"testing"
	"time"

	cerrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/email"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
