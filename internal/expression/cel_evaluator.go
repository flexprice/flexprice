package expression

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	celast "github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/parser"
	"github.com/shopspring/decimal"
)

// reservedIds are CEL reserved keywords - not valid as variable names from event properties.
var reservedIds = map[string]struct{}{
	"as": {}, "break": {}, "const": {}, "continue": {}, "else": {},
	"false": {}, "for": {}, "function": {}, "if": {}, "import": {},
	"in": {}, "let": {}, "loop": {}, "package": {}, "namespace": {},
	"null": {}, "return": {}, "true": {}, "var": {}, "void": {}, "while": {},
	"__result__": {}, // Hidden accumulator in comprehensions
}

// Evaluator evaluates CEL expressions to compute quantity from event properties.
type Evaluator interface {
	EvaluateQuantity(expr string, properties map[string]interface{}) (decimal.Decimal, error)
}

// CELEvaluator implements Evaluator using CEL with caching of compiled programs.
type CELEvaluator struct {
	cache sync.Map // expression string -> *cel.Program
}

// NewCELEvaluator creates a new CEL-based expression evaluator.
func NewCELEvaluator() *CELEvaluator {
	return &CELEvaluator{}
}

// EvaluateQuantity evaluates the CEL expression with the given properties and returns the result as a decimal.
// Property names are used directly in the expression (e.g., token * duration * pixel).
// Missing properties are treated as 0.
func (e *CELEvaluator) EvaluateQuantity(expr string, properties map[string]interface{}) (decimal.Decimal, error) {
	if expr == "" {
		return decimal.Zero, fmt.Errorf("expression is empty")
	}

	prg, err := e.getOrCompile(expr)
	if err != nil {
		return decimal.Zero, err
	}

	// Build activation: pre-fill missing identifiers with 0
	activation := e.buildActivation(expr, properties)

	out, _, err := prg.Eval(activation)
	if err != nil {
		return decimal.Zero, fmt.Errorf("CEL eval: %w", err)
	}

	if out == nil {
		return decimal.Zero, fmt.Errorf("expression result is nil")
	}

	// Handle CEL error result
	if types.IsError(out) {
		return decimal.Zero, fmt.Errorf("expression error: %v", out)
	}

	return toDecimal(out.Value())
}

// getOrCompile returns a cached program or compiles and caches the expression.
func (e *CELEvaluator) getOrCompile(expr string) (cel.Program, error) {
	if cached, ok := e.cache.Load(expr); ok {
		return cached.(cel.Program), nil
	}

	prg, err := e.compile(expr)
	if err != nil {
		return nil, err
	}

	e.cache.Store(expr, prg)
	return prg, nil
}

// compile parses the expression, extracts identifiers, and compiles a CEL program.
func (e *CELEvaluator) compile(expr string) (cel.Program, error) {
	source := common.NewStringSource(expr, "expression")
	parsed, errs := parser.Parse(source)
	if errs != nil && len(errs.GetErrors()) > 0 {
		return nil, fmt.Errorf("parse: %s", errs.ToDisplayString())
	}

	identifiers := extractIdentifiers(parsed.Expr())
	if len(identifiers) == 0 {
		return nil, fmt.Errorf("expression has no variable identifiers")
	}

	// Build env with each identifier as Dyn
	opts := make([]cel.EnvOption, 0, len(identifiers))
	for _, id := range identifiers {
		opts = append(opts, cel.Variable(id, cel.DynType))
	}

	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("env: %w", err)
	}

	ast, iss := env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("compile: %w", iss.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("program: %w", err)
	}

	return prg, nil
}

// extractIdentifiers walks the AST and collects unique identifier names (excluding reserved).
func extractIdentifiers(expr celast.Expr) []string {
	seen := make(map[string]struct{})
	var ids []string

	visitor := celast.NewExprVisitor(func(e celast.Expr) {
		if e.Kind() == celast.IdentKind {
			name := e.AsIdent()
			if _, reserved := reservedIds[name]; reserved {
				return
			}
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				ids = append(ids, name)
			}
		}
	})

	celast.PostOrderVisit(expr, visitor)
	return ids
}

// buildActivation creates the activation map from properties, pre-filling missing identifiers with 0.
// We need the identifiers from the expression - we'll extract them again or pass them.
// For simplicity, we merge properties with a default 0 for any key we might need.
// Actually we don't know identifiers at eval time without re-parsing. We could:
// 1. Store identifiers per expression in cache (expression -> (program, identifiers))
// 2. Or just pass properties as-is and let CEL error on missing - then catch and retry with 0?
// 3. Or pre-fill all keys from properties, and for any activation lookup failure we can't easily fix.
//
// The simplest: pass properties as-is. If a key is missing, CEL will return an error.
// We can document that users should ensure all keys exist, or we add a get(key, default) function.
// For MVP: clone properties and ensure we have at least empty values. Actually CEL might
// treat missing key as "no such attribute" error. Let me check - we need to pre-fill.
//
// We need to store identifiers alongside the program. Let me change the cache to store (program, identifiers).
type cacheEntry struct {
	prg         *cel.Program
	identifiers []string
}

func (e *CELEvaluator) buildActivation(expr string, properties map[string]interface{}) map[string]interface{} {
	// Get identifiers - we need them. Re-parse to extract (or store in cache).
	// For now re-parse - it's fast. Alternatively we could change getOrCompile to return identifiers too.
	source := common.NewStringSource(expr, "expression")
	parsed, errs := parser.Parse(source)
	if errs != nil && len(errs.GetErrors()) > 0 {
		if properties != nil {
			return properties
		}
		return map[string]interface{}{}
	}
	identifiers := extractIdentifiers(parsed.Expr())

	activation := make(map[string]interface{}, len(identifiers))
	if properties != nil {
		for k, v := range properties {
			activation[k] = v
		}
	}

	// Pre-fill missing identifiers with 0
	for _, id := range identifiers {
		if _, ok := activation[id]; !ok {
			activation[id] = 0
		}
	}

	return activation
}

// toDecimal converts CEL result (from ref.Val.Value()) to decimal.Decimal.
func toDecimal(val interface{}) (decimal.Decimal, error) {
	if val == nil {
		return decimal.Zero, fmt.Errorf("expression result is nil")
	}

	switch v := val.(type) {
	case float64:
		return decimal.NewFromFloat(v), nil
	case int64:
		return decimal.NewFromInt(v), nil
	case int:
		return decimal.NewFromInt(int64(v)), nil
	case uint64:
		return decimal.NewFromUint64(v), nil
	default:
		return decimal.Zero, fmt.Errorf("expression must evaluate to a number, got %T", val)
	}
}
