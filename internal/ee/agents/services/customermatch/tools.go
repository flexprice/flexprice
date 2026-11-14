package customermatch

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/flexprice/flexprice/internal/ee/agents/providers/openrouter"
	"github.com/samber/lo"
)

type toolOutput struct {
	Customers []CustomerRecord `json:"customers,omitempty"`
	Customer  *CustomerRecord  `json:"customer,omitempty"`
	Count     *int             `json:"count,omitempty"`
	Error     string           `json:"error,omitempty"`
}

func (s *CustomerMatchService) tools() []openrouter.Tool {
	return []openrouter.Tool{
		s.tool(toolSearchByName,
			"Find customers whose name contains the query, case-insensitive. Call it again with variants of the name.",
			s.object(map[string]any{
				"query": s.str("Part of a company name, at least 2 characters"),
				"limit": map[string]any{"type": []string{"integer", "null"}, "description": "Maximum results, 1-20; null for 10"},
			})),
		s.tool(toolSearchByEmail,
			"Find customers by exact email address, or every customer whose email is at a company domain (example.com).",
			s.object(map[string]any{
				"email_or_domain": s.str("An email address or a bare domain"),
			})),
		s.tool(toolGetCustomer,
			"Get one customer's details to confirm a candidate.",
			s.object(map[string]any{
				"customer_id": s.str("A customer id returned by a search"),
			})),
		s.tool(toolSubmitMatch,
			"Submit the final answer. Call exactly once.",
			s.object(map[string]any{
				"best_customer_id": map[string]any{
					"type":        []string{"string", "null"},
					"description": "The matching customer id, or null when there is no single confident match",
				},
				"candidates": map[string]any{
					"type":        "array",
					"description": "Plausible customers, best first; empty when nothing fits",
					"items": s.object(map[string]any{
						"customer_id": s.str("A customer id returned by a tool"),
						"confidence":  map[string]any{"type": "integer", "description": "0-100"},
						"evidence": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string", "enum": evidenceKinds},
						},
						"reason": s.str("One short sentence explaining the score"),
					}),
				},
			})),
	}
}

func (s *CustomerMatchService) tool(name, description string, params map[string]any) openrouter.Tool {
	raw, _ := json.Marshal(params)
	return openrouter.Tool{
		Type: openrouter.ToolTypeFunction,
		Function: openrouter.ToolFunction{
			Name:        name,
			Description: description,
			Parameters:  raw,
			Strict:      true,
		},
	}
}

func (s *CustomerMatchService) object(props map[string]any) map[string]any {
	required := lo.Keys(props)
	sort.Strings(required)
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func (s *CustomerMatchService) str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

// runTool executes one lookup tool and returns its JSON result. Bad arguments and lookup failures go back
// to the model as an error result so it can try again; only a cancelled context aborts the run.
func (s *CustomerMatchService) runTool(ctx context.Context, call openrouter.ToolCall, directory CustomerDirectory, seen map[string]bool) (string, error) {
	out := s.dispatch(ctx, call, directory)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, c := range out.Customers {
		seen[c.ID] = true
	}
	if out.Customer != nil {
		seen[out.Customer.ID] = true
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (s *CustomerMatchService) dispatch(ctx context.Context, call openrouter.ToolCall, directory CustomerDirectory) toolOutput {
	switch call.Function.Name {
	case toolSearchByName:
		var args nameSearchArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return toolOutput{Error: "invalid arguments"}
		}
		query := strings.TrimSpace(args.Query)
		if len([]rune(query)) < minQueryLength || len([]rune(query)) > maxQueryLength {
			return toolOutput{Error: "query must be 2 to 100 characters"}
		}
		return s.listOutput(directory.SearchByName(ctx, query, s.limit(args.Limit)))
	case toolSearchByEmail:
		var args emailSearchArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return toolOutput{Error: "invalid arguments"}
		}
		value := strings.ToLower(strings.TrimSpace(args.EmailOrDomain))
		if value == "" || len(value) > maxQueryLength {
			return toolOutput{Error: "email_or_domain is required"}
		}
		return s.listOutput(directory.SearchByEmail(ctx, value, maxSearchLimit))
	case toolGetCustomer:
		var args getCustomerArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || strings.TrimSpace(args.CustomerID) == "" {
			return toolOutput{Error: "customer_id is required"}
		}
		record, err := directory.Get(ctx, strings.TrimSpace(args.CustomerID))
		if err != nil || record == nil {
			return toolOutput{Error: "customer not found"}
		}
		return toolOutput{Customer: record}
	default:
		return toolOutput{Error: "unknown tool " + call.Function.Name}
	}
}

func (s *CustomerMatchService) listOutput(records []CustomerRecord, err error) toolOutput {
	if err != nil {
		return toolOutput{Error: "search failed, try a different query"}
	}
	return toolOutput{Customers: records, Count: lo.ToPtr(len(records))}
}

func (s *CustomerMatchService) limit(requested int) int {
	if requested <= 0 {
		return defaultSearchLimit
	}
	return min(requested, maxSearchLimit)
}
