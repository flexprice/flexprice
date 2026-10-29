package dto

import (
	"encoding/json"
	"strings"
	"testing"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestBillingTermsJSON_RejectsUnsupportedControls(t *testing.T) {
	for _, body := range []string{
		`{"expected_state":"state","reason":"correct","billing_terms":{"rows":[]},"replace_invoice_ids":["inv_1"]}`,
		`{"expected_state":"state","reason":"correct","owner_type":"customer","billing_terms":{"rows":[]}}`,
		`{"expected_state":"state","reason":"correct","billing_terms":{"version":2,"rows":[]}}`,
		`{"expected_state":"state","reason":"correct","billing_terms":{"rows":[{"kind":"once","value_type":"fixed","price_id":"price_1"}]}}`,
	} {
		var request ReplaceBillingTermsRequest
		require.Error(t, json.Unmarshal([]byte(body), &request))
	}
	var preview PreviewBillingTermsRequest
	require.NoError(t, json.Unmarshal([]byte(`{"reason":"correct","billing_terms":{"rows":[{"kind":"once","value_type":"fixed","percent":"25","on_trigger":true}]},"replace_invoice_ids":["inv_1"]}`), &preview))
	require.NoError(t, preview.Validate())
	require.Equal(t, "25", preview.BillingTerms.RuleInputs()[0].Percent.String())
	require.True(t, preview.BillingTerms.RuleInputs()[0].OnTrigger)
}

func TestBillingTermsRequestBounds(t *testing.T) {
	valid := ReplaceBillingTermsRequest{ExpectedState: "state", Reason: "Correction", BillingTerms: BillingTermsInput{Rows: []BillingTermInput{{Kind: types.BillingTermOnce}}}}
	for _, tc := range []struct {
		name   string
		change func(*ReplaceBillingTermsRequest)
	}{
		{"missing token", func(r *ReplaceBillingTermsRequest) { r.ExpectedState = " " }},
		{"large token", func(r *ReplaceBillingTermsRequest) { r.ExpectedState = strings.Repeat("s", 129) }},
		{"missing reason", func(r *ReplaceBillingTermsRequest) { r.Reason = " " }},
		{"large reason", func(r *ReplaceBillingTermsRequest) { r.Reason = strings.Repeat("語", 256) }},
		{"empty rules", func(r *ReplaceBillingTermsRequest) { r.BillingTerms.Rows = nil }},
		{"too many rules", func(r *ReplaceBillingTermsRequest) { r.BillingTerms.Rows = make([]BillingTermInput, 21) }},
	} {
		t.Run(tc.name, func(t *testing.T) { request := valid; tc.change(&request); require.Error(t, request.Validate()) })
	}
	require.NoError(t, valid.Validate())
	valid.Reason = strings.Repeat("語", 255)
	require.NoError(t, valid.Validate())
}

func TestBillingSchedulePageBounds(t *testing.T) {
	request := ListBillingScheduleItemsRequest{}
	require.NoError(t, request.Normalize())
	require.Equal(t, 50, request.Limit)
	for _, request := range []ListBillingScheduleItemsRequest{{Limit: -1}, {Limit: 101}, {Cursor: strings.Repeat("a", 2049)}, {ScheduleStatus: "paid"}} {
		require.Error(t, request.Normalize())
	}
}

func TestBillingScheduleJSON_ProposedIDAbsentAndUnresolvedDateNull(t *testing.T) {
	data, err := json.Marshal(BillingScheduleItemResponse{ScheduleItem: &domain.ScheduleItem{}})
	require.NoError(t, err)
	var object map[string]any
	require.NoError(t, json.Unmarshal(data, &object))
	require.NotContains(t, object, "id")
	require.Contains(t, object, "bill_on")
	require.Nil(t, object["bill_on"])
}
