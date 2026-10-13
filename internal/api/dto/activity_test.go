package dto

import (
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestListActivityRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      ListActivityRequest
		wantHint string
	}{
		{"entity_id without entity_type", ListActivityRequest{EntityID: "inv_1"}, "entity_type is required when entity_id is set"},
		{"limit over max", ListActivityRequest{Limit: 101}, "limit can be at most 100 (50 with expand state)"},
		{"limit over max with state", ListActivityRequest{Limit: 60, Expand: []string{"state"}}, "limit can be at most 100 (50 with expand state)"},
		{"unknown actor type", ListActivityRequest{ActorTypes: []string{"robot"}}, "Unknown actor type"},
		{"unknown expand", ListActivityRequest{Expand: []string{"diff"}}, "Unknown expand"},
		{"valid", ListActivityRequest{EntityType: "invoice", EntityID: "inv_1", Limit: 50, Expand: []string{"state"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantHint == "" {
				require.NoError(t, err)
				return
			}
			require.True(t, ierr.IsValidation(err))
			require.Contains(t, strings.Join(errors.GetAllHints(err), " "), tt.wantHint)
		})
	}
}
