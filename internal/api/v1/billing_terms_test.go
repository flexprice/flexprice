package v1

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type billingTermsHandlerService struct {
	service.CommitmentBillingTermsService
	called  string
	owner   string
	item    string
	request any
	err     error
}

func (s *billingTermsHandlerService) Get(_ context.Context, id string) (*dto.BillingTermsStateResponse, error) {
	s.called = "get"
	s.owner = id
	return &dto.BillingTermsStateResponse{StateToken: "fresh"}, s.err
}
func (s *billingTermsHandlerService) Preview(_ context.Context, id string, r dto.PreviewBillingTermsRequest) (*dto.BillingTermsPreviewResponse, error) {
	s.called = "preview"
	s.owner = id
	s.request = r
	return &dto.BillingTermsPreviewResponse{}, s.err
}
func (s *billingTermsHandlerService) Replace(_ context.Context, id string, r dto.ReplaceBillingTermsRequest) (*dto.BillingTermsStateResponse, error) {
	s.called = "replace"
	s.owner = id
	s.request = r
	return &dto.BillingTermsStateResponse{}, s.err
}
func (s *billingTermsHandlerService) ListItems(_ context.Context, id string, r dto.ListBillingScheduleItemsRequest) (*dto.ListBillingScheduleItemsResponse, error) {
	s.called = "list"
	s.owner = id
	s.request = r
	return &dto.ListBillingScheduleItemsResponse{}, s.err
}
func (s *billingTermsHandlerService) History(_ context.Context, id string, r dto.ListBillingScheduleItemsRequest) (*dto.ListBillingScheduleItemsResponse, error) {
	s.called = "history"
	s.owner = id
	s.request = r
	return &dto.ListBillingScheduleItemsResponse{}, s.err
}
func (s *billingTermsHandlerService) GetItem(_ context.Context, id, item string) (*dto.BillingScheduleItemResponse, error) {
	s.called = "item"
	s.owner = id
	s.item = item
	return &dto.BillingScheduleItemResponse{}, s.err
}
func (s *billingTermsHandlerService) Trigger(_ context.Context, id, item string, r dto.TriggerBillingScheduleItemRequest) (*dto.TriggerBillingScheduleItemResponse, error) {
	s.called = "trigger"
	s.owner = id
	s.item = item
	s.request = r
	return &dto.TriggerBillingScheduleItemResponse{}, s.err
}

func TestBillingTermsHandlers_DelegateScopedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, method, body, query string
		invoke                    func(BillingTermsHandler, *gin.Context)
	}{
		{name: "get", method: "GET", invoke: func(h BillingTermsHandler, c *gin.Context) { h.Get(c) }},
		{name: "preview", method: "POST", body: `{"reason":"correct","billing_terms":{"rows":[]},"replace_invoice_ids":["invoice"]}`, invoke: func(h BillingTermsHandler, c *gin.Context) { h.Preview(c) }},
		{name: "replace", method: "PUT", body: `{"expected_state":"reviewed","reason":"correct","billing_terms":{"rows":[]}}`, invoke: func(h BillingTermsHandler, c *gin.Context) { h.Replace(c) }},
		{name: "list", method: "GET", query: "?limit=25&cursor=opaque&include_archived=true&schedule_status=invoiced", invoke: func(h BillingTermsHandler, c *gin.Context) { h.ListItems(c) }},
		{name: "history", method: "GET", query: "?limit=25&cursor=opaque", invoke: func(h BillingTermsHandler, c *gin.Context) { h.History(c) }},
		{name: "item", method: "GET", invoke: func(h BillingTermsHandler, c *gin.Context) { h.GetItem(c) }},
		{name: "trigger", method: "POST", body: `{"expected_state":"reviewed"}`, invoke: func(h BillingTermsHandler, c *gin.Context) { h.Trigger(c) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &billingTermsHandlerService{}
			handler := NewBillingTermsHandler(svc)
			writer := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(writer)
			ctx.Request = httptest.NewRequest(tc.method, "/"+tc.query, strings.NewReader(tc.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Params = gin.Params{{Key: "id", Value: "comm_route"}, {Key: "item_id", Value: "item_route"}}
			tc.invoke(handler, ctx)
			require.Empty(t, ctx.Errors)
			require.Equal(t, 200, writer.Code)
			require.Equal(t, tc.name, svc.called)
			require.Equal(t, "comm_route", svc.owner)
			if tc.name == "item" || tc.name == "trigger" {
				require.Equal(t, "item_route", svc.item)
			}
			if tc.name == "list" {
				require.Equal(t, 25, svc.request.(dto.ListBillingScheduleItemsRequest).Limit)
				require.True(t, svc.request.(dto.ListBillingScheduleItemsRequest).IncludeArchived)
			}
			if tc.name == "replace" {
				require.Equal(t, "reviewed", svc.request.(dto.ReplaceBillingTermsRequest).ExpectedState)
			}
			if tc.name == "preview" {
				require.Equal(t, []string{"invoice"}, svc.request.(dto.PreviewBillingTermsRequest).ReplaceInvoiceIDs)
			}
		})
	}
}

func TestBillingTermsHandlers_RejectUnsupportedMutationBeforeDelegating(t *testing.T) {
	for _, body := range []string{`{"replace_invoice_ids":["inv"]}`, `{"billing_terms":{"rows":[{"owner_id":"other"}]}}`, `{`} {
		svc := &billingTermsHandlerService{}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("PUT", "/", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		NewBillingTermsHandler(svc).Replace(ctx)
		require.Len(t, ctx.Errors, 1)
		require.Empty(t, svc.called)
	}
}

func TestBillingTermsHandlers_PropagateServiceError(t *testing.T) {
	expected := errors.New("stale state")
	svc := &billingTermsHandlerService{err: expected}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/", nil)
	NewBillingTermsHandler(svc).Get(ctx)
	require.ErrorIs(t, ctx.Errors.Last().Err, expected)
}
