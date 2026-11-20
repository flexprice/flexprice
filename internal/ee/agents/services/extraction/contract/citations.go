package contract

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/flexprice/flexprice/internal/ee/agents/providers/reducto"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

func (s *ContractExtractionService) toResult(raw json.RawMessage) (*extraction.Result, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, s.shapeError(err)
	}

	converted, err := json.Marshal(s.convert(tree, nil))
	if err != nil {
		return nil, s.shapeError(err)
	}
	var result extraction.Result
	if err := json.Unmarshal(converted, &result); err != nil {
		return nil, s.shapeError(err)
	}
	return &result, nil
}

func (s *ContractExtractionService) convert(node any, path []string) any {
	switch n := node.(type) {
	case map[string]any:
		if s.isField(n) {
			return s.fieldValue(n, path)
		}
		out := make(map[string]any, len(n))
		for k, v := range n {
			out[k] = s.convert(v, append(append([]string{}, path...), k))
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, v := range n {
			out[i] = s.convert(v, append(append([]string{}, path...), "[]"))
		}
		return out
	default:
		return n
	}
}

func (s *ContractExtractionService) isField(n map[string]any) bool {
	_, hasValue := n["value"]
	_, hasCitations := n["citations"]
	return hasValue && hasCitations
}

func (s *ContractExtractionService) fieldValue(n map[string]any, path []string) any {
	b, _ := json.Marshal(n)
	var f reducto.Field
	_ = json.Unmarshal(b, &f)

	if !s.isPlain(path) {
		return s.toTerm(f)
	}
	if last := path[len(path)-1]; last == "page_start" || last == "page_end" {
		n, _ := strconv.Atoi(s.valueString(f.Value))
		return n
	}
	return s.valueString(f.Value)
}

func (s *ContractExtractionService) isPlain(path []string) bool {
	if len(path) == 0 {
		return false
	}
	switch path[0] {
	case "documents", "doubts":
		return true
	}
	return lo.Contains(path, "tiers") || (path[0] == "charges" && path[len(path)-1] == "kind")
}

func (s *ContractExtractionService) toTerm(f reducto.Field) extraction.Term {
	t := extraction.Term{Value: s.valueString(f.Value), Options: []string{}}
	if len(f.Citations) == 0 {
		return t
	}

	first := f.Citations[0]
	t.Quote = strings.TrimSpace(first.Content)
	if first.BBox != nil {
		t.Page = first.BBox.Page
	}
	t.Confidence = s.confidence(f.Citations)
	for _, c := range f.Citations {
		if c.BBox != nil {
			t.Boxes = append(t.Boxes, extraction.Box{Page: c.BBox.Page, Left: c.BBox.Left, Top: c.BBox.Top, Width: c.BBox.Width, Height: c.BBox.Height})
		}
	}
	return t
}

func (s *ContractExtractionService) confidence(citations []reducto.Citation) int {
	lowest := maxConfidence
	for _, c := range citations {
		score := highLabelConfidence
		switch {
		case c.GranularConfidence != nil && c.GranularConfidence.ExtractConfidence != nil:
			score = int(math.Round(*c.GranularConfidence.ExtractConfidence * maxConfidence))
		case c.Confidence == reducto.ConfidenceLow:
			score = lowLabelConfidence
		}
		lowest = min(lowest, score)
	}
	return lowest
}

func (s *ContractExtractionService) valueString(raw json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if len(raw) == 0 || dec.Decode(&v) != nil {
		return ""
	}
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	default:
		return string(raw)
	}
}

func (s *ContractExtractionService) shapeError(err error) error {
	return ierr.WithError(err).
		WithHint("The document service returned a result in the wrong shape").
		Mark(ierr.ErrHTTPClient)
}
