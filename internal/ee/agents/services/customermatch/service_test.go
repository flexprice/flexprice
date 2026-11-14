package customermatch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	cerrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/agents/providers/openrouter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

type fakeLLM struct {
	replies    []*openrouter.Response
	requests   []*openrouter.Request
	configured bool
	err        error
}

func (f *fakeLLM) Complete(_ context.Context, req *openrouter.Request) (*openrouter.Response, error) {
	snapshot := *req
	snapshot.Messages = append([]openrouter.Message{}, req.Messages...)
	f.requests = append(f.requests, &snapshot)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.replies) == 0 {
		return &openrouter.Response{Model: "test/model", Content: "thinking"}, nil
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return r, nil
}

func (f *fakeLLM) DefaultModel() string { return "default/model" }
func (f *fakeLLM) IsConfigured() bool   { return f.configured }

type fakeDirectory struct {
	customers []CustomerRecord
	nameCalls []string
	err       error
}

func (d *fakeDirectory) SearchByName(_ context.Context, query string, limit int) ([]CustomerRecord, error) {
	d.nameCalls = append(d.nameCalls, query)
	if d.err != nil {
		return nil, d.err
	}
	var out []CustomerRecord
	for _, c := range d.customers {
		if strings.Contains(strings.ToLower(c.Name), strings.ToLower(query)) && len(out) < limit {
			out = append(out, c)
		}
	}
	return out, nil
}

func (d *fakeDirectory) SearchByEmail(_ context.Context, value string, _ int) ([]CustomerRecord, error) {
	var out []CustomerRecord
	for _, c := range d.customers {
		if c.Email == value || strings.HasSuffix(c.Email, "@"+value) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (d *fakeDirectory) Get(_ context.Context, id string) (*CustomerRecord, error) {
	for _, c := range d.customers {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, errors.New("not found")
}

func directory() *fakeDirectory {
	return &fakeDirectory{customers: []CustomerRecord{
		{ID: "cust_supp", Name: "Suppmayer GmbH", Email: "billing@suppmayer.de", Country: "DE"},
		{ID: "cust_supp_us", Name: "Suppmayer Inc", Email: "ap@suppmayer.com", Country: "US"},
		{ID: "cust_other", Name: "Northwind Traders"},
	}}
}

func call(id, name string, args any) openrouter.ToolCall {
	b, _ := json.Marshal(args)
	return openrouter.ToolCall{ID: id, Type: openrouter.ToolTypeFunction, Function: openrouter.ToolCallFunction{Name: name, Arguments: string(b)}}
}

func toolReply(calls ...openrouter.ToolCall) *openrouter.Response {
	return &openrouter.Response{Model: "test/model", ToolCalls: calls, FinishReason: openrouter.FinishReasonToolCalls,
		Usage: &openrouter.Usage{PromptTokens: 100, CompletionTokens: 10, CostUSD: 0.001}}
}

func submit(best *string, candidates ...Candidate) openrouter.ToolCall {
	return call("call_submit", toolSubmitMatch, submission{BestCustomerID: best, Candidates: candidates})
}

func strPtr(s string) *string { return &s }

func newService(llm *fakeLLM, cfg config.CustomerMatchConfig) *CustomerMatchService {
	return NewCustomerMatchService(llm, &config.Configuration{CustomerMatch: cfg}, logger.NewNoopLogger()).(*CustomerMatchService)
}

func request() *MatchRequest {
	return &MatchRequest{LegalName: "Süppmayer GmbH", BillingEmail: "finance@suppmayer.de"}
}

func TestMatch_SearchesThenSubmits(t *testing.T) {
	llm := &fakeLLM{configured: true, replies: []*openrouter.Response{
		toolReply(call("c1", toolSearchByName, nameSearchArgs{Query: "Suppmayer"})),
		toolReply(call("c2", toolGetCustomer, getCustomerArgs{CustomerID: "cust_supp"})),
		toolReply(submit(strPtr("cust_supp"),
			Candidate{CustomerID: "cust_supp_us", Confidence: 45, Evidence: []string{EvidenceNameVariant}, Reason: "Same name, US entity"},
			Candidate{CustomerID: "cust_supp", Confidence: 93, Evidence: []string{EvidenceNameVariant, EvidenceEmailDomain}, Reason: "Name and domain match"},
		)),
	}}
	dir := directory()

	res, err := newService(llm, config.CustomerMatchConfig{Model: "match/model"}).Match(context.Background(), request(), dir)
	require.NoError(t, err)
	require.Equal(t, "cust_supp", res.BestCustomerID)
	require.Equal(t, []string{"cust_supp", "cust_supp_us"}, []string{res.Candidates[0].CustomerID, res.Candidates[1].CustomerID})
	require.Equal(t, 3, res.Steps)
	require.Equal(t, 2, res.ToolCalls)
	require.Equal(t, 300, res.Usage.PromptTokens)
	require.InDelta(t, 0.003, res.Usage.CostUSD, 1e-9)
	require.Equal(t, []string{"Suppmayer"}, dir.nameCalls)

	first := llm.requests[0]
	require.Equal(t, "match/model", first.Model)
	require.Len(t, first.Tools, 4)
	require.Equal(t, openrouter.ToolChoiceRequired, first.ToolChoice.Mode)

	last := llm.requests[2].Messages
	toolMsg := last[len(last)-1]
	require.Equal(t, openrouter.RoleTool, toolMsg.Role)
	require.Equal(t, "c2", toolMsg.ToolCallID)
	require.Contains(t, toolMsg.Content[0].Text, `"id":"cust_supp"`)
}

func TestMatch_DropsIDsTheToolsNeverReturned(t *testing.T) {
	llm := &fakeLLM{configured: true, replies: []*openrouter.Response{
		toolReply(call("c1", toolSearchByName, nameSearchArgs{Query: "Northwind"})),
		toolReply(submit(strPtr("cust_invented"),
			Candidate{CustomerID: "cust_invented", Confidence: 99},
			Candidate{CustomerID: "cust_other", Confidence: 150, Evidence: []string{"made_up", EvidenceExactName, EvidenceExactName}},
		)),
	}}

	res, err := newService(llm, config.CustomerMatchConfig{Model: "m"}).Match(context.Background(), request(), directory())
	require.NoError(t, err)
	require.Empty(t, res.BestCustomerID)
	require.Len(t, res.Candidates, 1)
	require.Equal(t, Candidate{CustomerID: "cust_other", Confidence: 100, Evidence: []string{EvidenceExactName}}, res.Candidates[0])
}

func TestMatch_NoMatchIsAValidAnswer(t *testing.T) {
	llm := &fakeLLM{configured: true, replies: []*openrouter.Response{
		toolReply(call("c1", toolSearchByEmail, emailSearchArgs{EmailOrDomain: "nobody.io"})),
		toolReply(submit(nil)),
	}}

	res, err := newService(llm, config.CustomerMatchConfig{Model: "m"}).Match(context.Background(), request(), directory())
	require.NoError(t, err)
	require.Empty(t, res.BestCustomerID)
	require.Empty(t, res.Candidates)
}

func TestMatch_ForcesSubmitWhenToolBudgetIsSpent(t *testing.T) {
	llm := &fakeLLM{configured: true, replies: []*openrouter.Response{
		toolReply(call("c1", toolSearchByName, nameSearchArgs{Query: "Supp"}), call("c2", toolSearchByName, nameSearchArgs{Query: "mayer"})),
		toolReply(submit(nil)),
	}}

	_, err := newService(llm, config.CustomerMatchConfig{Model: "m", MaxToolCalls: 2}).Match(context.Background(), request(), directory())
	require.NoError(t, err)
	require.Len(t, llm.requests, 2)
	require.Equal(t, toolSubmitMatch, llm.requests[1].ToolChoice.Function)
}

func TestMatch_StopsAfterMaxStepsWithOneForcedSubmit(t *testing.T) {
	llm := &fakeLLM{configured: true}

	res, err := newService(llm, config.CustomerMatchConfig{Model: "m", MaxSteps: 3}).Match(context.Background(), request(), directory())
	require.NoError(t, err)
	require.Len(t, llm.requests, 4)
	require.Equal(t, toolSubmitMatch, llm.requests[3].ToolChoice.Function)
	require.Empty(t, res.Candidates)
	require.Equal(t, submitReminder, llm.requests[1].Messages[len(llm.requests[1].Messages)-1].Content[0].Text)
}

func TestMatch_ToolProblemsGoBackToTheModel(t *testing.T) {
	llm := &fakeLLM{configured: true, replies: []*openrouter.Response{
		toolReply(
			call("c1", toolSearchByName, nameSearchArgs{Query: "a"}),
			call("c2", toolGetCustomer, getCustomerArgs{CustomerID: "cust_missing"}),
			openrouter.ToolCall{ID: "c3", Function: openrouter.ToolCallFunction{Name: toolSearchByEmail, Arguments: "{not json"}},
			call("c4", "drop_table", map[string]string{}),
		),
		toolReply(submit(nil)),
	}}

	_, err := newService(llm, config.CustomerMatchConfig{Model: "m"}).Match(context.Background(), request(), directory())
	require.NoError(t, err)
	msgs := llm.requests[1].Messages
	results := map[string]string{}
	for _, m := range msgs {
		if m.Role == openrouter.RoleTool {
			results[m.ToolCallID] = m.Content[0].Text
		}
	}
	require.Contains(t, results["c1"], "2 to 100 characters")
	require.Contains(t, results["c2"], "customer not found")
	require.Contains(t, results["c3"], "invalid arguments")
	require.Contains(t, results["c4"], "unknown tool")
}

func TestMatch_SearchFailureIsReportedToTheModel(t *testing.T) {
	llm := &fakeLLM{configured: true, replies: []*openrouter.Response{
		toolReply(call("c1", toolSearchByName, nameSearchArgs{Query: "Supp"})),
		toolReply(submit(nil)),
	}}
	dir := directory()
	dir.err = errors.New("db down")

	_, err := newService(llm, config.CustomerMatchConfig{Model: "m"}).Match(context.Background(), request(), dir)
	require.NoError(t, err)
	msgs := llm.requests[1].Messages
	require.Contains(t, msgs[len(msgs)-1].Content[0].Text, "search failed")
	require.NotContains(t, msgs[len(msgs)-1].Content[0].Text, "db down")
}

func TestMatch_Errors(t *testing.T) {
	tests := []struct {
		name string
		llm  *fakeLLM
		req  *MatchRequest
		dir  CustomerDirectory
		mark error
	}{
		{name: "nothing to match", llm: &fakeLLM{configured: true}, req: &MatchRequest{BillingAddress: "Berlin"}, dir: directory(), mark: ierr.ErrValidation},
		{name: "not configured", llm: &fakeLLM{}, req: request(), dir: directory(), mark: ierr.ErrServiceUnavailable},
		{name: "no directory", llm: &fakeLLM{configured: true}, req: request(), dir: nil, mark: ierr.ErrInternal},
		{name: "provider error", llm: &fakeLLM{configured: true, err: ierr.NewError("x").Mark(ierr.ErrTooManyRequests)}, req: request(), dir: directory(), mark: ierr.ErrTooManyRequests},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newService(tt.llm, config.CustomerMatchConfig{Model: "m"}).Match(context.Background(), tt.req, tt.dir)
			require.Error(t, err)
			require.True(t, cerrors.Is(err, tt.mark), err.Error())
		})
	}
}

func TestMatch_FallsBackToClientDefaultModel(t *testing.T) {
	llm := &fakeLLM{configured: true, replies: []*openrouter.Response{toolReply(submit(nil))}}
	_, err := newService(llm, config.CustomerMatchConfig{}).Match(context.Background(), request(), directory())
	require.NoError(t, err)
	require.Equal(t, "default/model", llm.requests[0].Model)
}

func TestTools_SchemasAreStrict(t *testing.T) {
	s := newService(&fakeLLM{}, config.CustomerMatchConfig{})
	for _, tool := range s.tools() {
		var schema map[string]any
		require.NoError(t, json.Unmarshal(tool.Function.Parameters, &schema))
		require.Equal(t, false, schema["additionalProperties"], tool.Function.Name)
		require.Len(t, schema["required"], len(schema["properties"].(map[string]any)), tool.Function.Name)
		require.True(t, tool.Function.Strict)
	}
}
