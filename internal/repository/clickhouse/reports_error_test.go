package clickhouse

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	cerrors "github.com/cockroachdb/errors"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
)

func TestReportError_MapsLimitExceptionsToValidation(t *testing.T) {
	for _, code := range []int32{158, 159, 241} {
		err := reportError(fmt.Errorf("wrapped: %w", &proto.Exception{Code: code}), "run report query")
		assert.True(t, ierr.IsValidation(err), code)
		assert.Equal(t, "Query too large; narrow the date range", cerrors.FlattenHints(err), code)
	}
}

func TestReportError_OtherErrorsStayDatabase(t *testing.T) {
	for _, base := range []error{&proto.Exception{Code: 60}, errors.New("boom")} {
		err := reportError(base, "run report query")
		assert.False(t, ierr.IsValidation(err))
		assert.True(t, ierr.IsDatabase(err))
	}
}
