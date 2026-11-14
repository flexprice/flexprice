package service

import (
	"errors"
	"testing"

	cerrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
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
