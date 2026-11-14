package customermatch

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/agents/providers/openrouter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/samber/lo"
)

type CustomerMatchService struct {
	client       openrouter.OpenRouterClient
	logger       *logger.Logger
	model        string
	maxSteps     int
	maxToolCalls int
	timeout      time.Duration
}

func NewCustomerMatchService(client openrouter.OpenRouterClient, cfg *config.Configuration, logger *logger.Logger) CustomerMatchServiceInterface {
	s := &CustomerMatchService{
		client:       client,
		logger:       logger,
		maxSteps:     defaultMaxSteps,
		maxToolCalls: defaultMaxToolCalls,
		timeout:      defaultTimeout,
	}
	if cfg == nil {
		return s
	}
	m := cfg.CustomerMatch
	s.model = strings.TrimSpace(m.Model)
	if m.MaxSteps > 0 {
		s.maxSteps = m.MaxSteps
	}
	if m.MaxToolCalls > 0 {
		s.maxToolCalls = m.MaxToolCalls
	}
	if m.TimeoutSeconds > 0 {
		s.timeout = time.Duration(m.TimeoutSeconds) * time.Second
	}
	return s
}

func (s *CustomerMatchService) IsConfigured() bool {
	return s.client != nil && s.client.IsConfigured() && s.modelName() != ""
}

func (s *CustomerMatchService) Match(ctx context.Context, req *MatchRequest, directory CustomerDirectory) (*MatchResult, error) {
	if err := s.validateRequest(req, directory); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	run := &matchRun{
		messages: []openrouter.Message{s.textMessage(openrouter.RoleSystem, systemPrompt), s.textMessage(openrouter.RoleUser, s.userPrompt(req))},
		seen:     map[string]bool{},
		result:   &MatchResult{Candidates: []Candidate{}, Usage: &openrouter.Usage{}},
	}

	for run.result.Steps < s.maxSteps {
		choice := openrouter.ToolChoice{Mode: openrouter.ToolChoiceRequired}
		if run.result.ToolCalls >= s.maxToolCalls {
			choice = openrouter.ToolChoice{Function: toolSubmitMatch}
		}
		done, err := s.step(ctx, run, directory, choice)
		if err != nil {
			return nil, err
		}
		if done {
			return s.finish(ctx, run), nil
		}
	}

	if _, err := s.step(ctx, run, directory, openrouter.ToolChoice{Function: toolSubmitMatch}); err != nil {
		return nil, err
	}
	return s.finish(ctx, run), nil
}

type matchRun struct {
	messages []openrouter.Message
	seen     map[string]bool
	result   *MatchResult
}

// step sends one turn to the model and runs the tools it calls. It reports done once submit_match is called.
func (s *CustomerMatchService) step(ctx context.Context, run *matchRun, directory CustomerDirectory, choice openrouter.ToolChoice) (bool, error) {
	resp, err := s.client.Complete(ctx, &openrouter.Request{
		Model:      s.modelName(),
		Messages:   run.messages,
		Tools:      s.tools(),
		ToolChoice: &choice,
	})
	if err != nil {
		return false, err
	}
	run.result.Steps++
	run.result.Model = resp.Model
	s.addUsage(run.result.Usage, resp.Usage)

	if len(resp.ToolCalls) == 0 {
		run.messages = append(run.messages,
			s.textMessage(openrouter.RoleAssistant, resp.Content),
			s.textMessage(openrouter.RoleUser, submitReminder))
		return false, nil
	}

	run.messages = append(run.messages, openrouter.Message{Role: openrouter.RoleAssistant, ToolCalls: resp.ToolCalls})
	for _, call := range resp.ToolCalls {
		if call.Function.Name == toolSubmitMatch {
			s.applySubmission(run, call.Function.Arguments)
			return true, nil
		}
		run.result.ToolCalls++
		output, err := s.runTool(ctx, call, directory, run.seen)
		if err != nil {
			return false, ierr.WithError(err).
				WithHint("Customer matching took too long").
				Mark(ierr.ErrHTTPClient)
		}
		run.messages = append(run.messages, openrouter.Message{
			Role:       openrouter.RoleTool,
			ToolCallID: call.ID,
			Content:    []openrouter.ContentPart{{Type: openrouter.ContentTypeText, Text: output}},
		})
	}
	return false, nil
}

// applySubmission keeps only candidates the tools actually returned, so the model cannot invent ids.
func (s *CustomerMatchService) applySubmission(run *matchRun, arguments string) {
	var sub submission
	if err := json.Unmarshal([]byte(arguments), &sub); err != nil {
		return
	}

	byID := map[string]Candidate{}
	for _, c := range sub.Candidates {
		id := strings.TrimSpace(c.CustomerID)
		if !run.seen[id] {
			continue
		}
		c.CustomerID = id
		c.Confidence = max(0, min(maxConfidence, c.Confidence))
		c.Evidence = lo.Uniq(lo.Filter(c.Evidence, func(e string, _ int) bool { return lo.Contains(evidenceKinds, e) }))
		c.Reason = strings.TrimSpace(c.Reason)
		if prev, ok := byID[id]; !ok || c.Confidence > prev.Confidence {
			byID[id] = c
		}
	}
	candidates := lo.Values(byID)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Confidence != candidates[j].Confidence {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		return candidates[i].CustomerID < candidates[j].CustomerID
	})
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}
	run.result.Candidates = candidates

	if sub.BestCustomerID != nil {
		best := strings.TrimSpace(*sub.BestCustomerID)
		if _, ok := byID[best]; ok {
			run.result.BestCustomerID = best
		}
	}
}

func (s *CustomerMatchService) finish(ctx context.Context, run *matchRun) *MatchResult {
	s.logger.Info(ctx, "customer match finished",
		"model", run.result.Model,
		"steps", run.result.Steps,
		"tool_calls", run.result.ToolCalls,
		"candidates", len(run.result.Candidates),
		"has_best", run.result.BestCustomerID != "")
	return run.result
}

func (s *CustomerMatchService) validateRequest(req *MatchRequest, directory CustomerDirectory) error {
	if directory == nil {
		return ierr.NewError("customer directory is required").
			WithHint("Customer matching is not set up").
			Mark(ierr.ErrInternal)
	}
	if req == nil || (strings.TrimSpace(req.LegalName) == "" && strings.TrimSpace(req.BillingEmail) == "") {
		return ierr.NewError("legal name or billing email is required").
			WithHint("The contract has no customer name or email to match").
			Mark(ierr.ErrValidation)
	}
	if !s.IsConfigured() {
		return ierr.NewError("customer matching not configured").
			WithHint("AI customer matching is not configured on this server").
			Mark(ierr.ErrServiceUnavailable)
	}
	return nil
}

func (s *CustomerMatchService) userPrompt(req *MatchRequest) string {
	b, _ := json.MarshalIndent(req, "", "  ")
	return "Customer details extracted from the contract:\n" + string(b)
}

func (s *CustomerMatchService) textMessage(role, text string) openrouter.Message {
	return openrouter.Message{Role: role, Content: []openrouter.ContentPart{{Type: openrouter.ContentTypeText, Text: text}}}
}

func (s *CustomerMatchService) modelName() string {
	if s.model != "" {
		return s.model
	}
	if s.client == nil {
		return ""
	}
	return s.client.DefaultModel()
}

func (s *CustomerMatchService) addUsage(total, u *openrouter.Usage) {
	if u == nil {
		return
	}
	total.PromptTokens += u.PromptTokens
	total.CompletionTokens += u.CompletionTokens
	total.CostUSD += u.CostUSD
}
