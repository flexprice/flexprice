package expression

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	celast "github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
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
	EvaluateQuantity(expr string, properties map[string]any) (decimal.Decimal, error)
	// Validate parses and compiles the expression so structural errors surface at
	// definition time (e.g. when a meter is created) rather than per-event at runtime.
	Validate(expr string) error
}

var (
	defaultEvaluatorOnce sync.Once
	defaultEvaluator     *CELEvaluator
)

// cacheEntry holds the compiled program and the identifiers extracted from the expression.
type cacheEntry struct {
	prg         cel.Program
	identifiers []string
}

// CELEvaluator implements Evaluator using CEL with caching of compiled programs.
type CELEvaluator struct {
	cache  sync.Map // expression string -> *cacheEntry
	parser *parser.Parser
}

// mathEnvOpts registers stdlib-backed math helpers in every per-expression
// env. Each function takes/returns CEL double; the existing toDecimal
// non-finite guard turns sqrt(-1)/pow overflow into a clean error.
var mathEnvOpts = []cel.EnvOption{
	binaryDoubleFn("max", math.Max),
	binaryDoubleFn("min", math.Min),
	binaryDoubleFn("pow", math.Pow),
	unaryDoubleFn("abs", math.Abs),
	unaryDoubleFn("ceil", math.Ceil),
	unaryDoubleFn("floor", math.Floor),
	unaryDoubleFn("round", math.Round),
	unaryDoubleFn("sqrt", math.Sqrt),
	unaryDoubleFn("log", math.Log),
}

func unaryDoubleFn(name string, fn func(float64) float64) cel.EnvOption {
	return cel.Function(name,
		cel.Overload(name+"_double",
			[]*cel.Type{cel.DoubleType},
			cel.DoubleType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				return types.Double(fn(float64(v.(types.Double))))
			}),
		),
	)
}

func binaryDoubleFn(name string, fn func(a, b float64) float64) cel.EnvOption {
	return cel.Function(name,
		cel.Overload(name+"_double_double",
			[]*cel.Type{cel.DoubleType, cel.DoubleType},
			cel.DoubleType,
			cel.BinaryBinding(func(a, b ref.Val) ref.Val {
				return types.Double(fn(float64(a.(types.Double)), float64(b.(types.Double))))
			}),
		),
	)
}

// NewCELEvaluator creates a new CEL-based expression evaluator.
func NewCELEvaluator() *CELEvaluator {
	defaultEvaluatorOnce.Do(func() {
		p, err := parser.NewParser()
		if err != nil {
			panic(fmt.Sprintf("cel parser init: %v", err))
		}
		defaultEvaluator = &CELEvaluator{
			parser: p,
		}
	})

	return defaultEvaluator
}

// EvaluateQuantity evaluates the CEL expression with the given properties and returns the result as a decimal.
// Property names are used directly in the expression (e.g., token * duration * pixel).
// Missing properties are treated as 0.
func (e *CELEvaluator) EvaluateQuantity(expr string, properties map[string]any) (decimal.Decimal, error) {
	if expr == "" {
		return decimal.Zero, fmt.Errorf("expression is empty")
	}

	entry, err := e.getOrCompile(expr)
	if err != nil {
		return decimal.Zero, err
	}

	// Build activation: coerce properties to float64, pre-fill missing identifiers with 0
	activation, err := e.buildActivation(entry.identifiers, properties)
	if err != nil {
		return decimal.Zero, err
	}

	out, _, err := entry.prg.Eval(activation)
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

// Validate parses and compiles the expression so that syntax, missing-identifier,
// and structural errors surface at definition time instead of at event-processing
// time. It does not evaluate the expression.
//
// Unlike EvaluateQuantity, Validate compiles directly without touching the program
// cache: it runs at meter creation, which may be called with many one-off (or
// invalid) expressions, and caching those would grow the shared cache unbounded.
func (e *CELEvaluator) Validate(expr string) error {
	if expr == "" {
		return fmt.Errorf("expression is empty")
	}
	_, err := e.compile(expr)
	return err
}

// getOrCompile returns a cached entry or compiles and caches the expression.
func (e *CELEvaluator) getOrCompile(expr string) (*cacheEntry, error) {
	if cached, ok := e.cache.Load(expr); ok {
		return cached.(*cacheEntry), nil
	}

	entry, err := e.compile(expr)
	if err != nil {
		return nil, err
	}

	e.cache.Store(expr, entry)
	return entry, nil
}

// compile parses the expression, extracts identifiers, and compiles a CEL program.
//
// Every identifier is declared as a double and every integer literal is promoted to
// a double before type-checking, so all arithmetic is real-valued: `total / 1000`
// evaluates to 1.5, not a silently-truncated 1. Because the environment is fully
// typed, non-numeric expressions (e.g. string concatenation, a boolean result, or
// modulo on doubles) are rejected here at compile time rather than per-event.
func (e *CELEvaluator) compile(expr string) (*cacheEntry, error) {
	source := common.NewStringSource(expr, "meterQuantityExpression")
	parsed, errs := e.parser.Parse(source)
	if errs != nil && len(errs.GetErrors()) > 0 {
		return nil, fmt.Errorf("parse: %s", errs.ToDisplayString())
	}

	identifiers := extractIdentifiers(parsed.Expr())
	if len(identifiers) == 0 {
		return nil, fmt.Errorf("expression has no variable identifiers")
	}

	// Declare every identifier as a double. ClearMacros keeps the env's parser
	// consistent with e.parser (which has no macros) so identifier extraction and
	// compilation see the same AST.
	opts := make([]cel.EnvOption, 0, len(identifiers)+1+len(mathEnvOpts))
	opts = append(opts, cel.ClearMacros())
	opts = append(opts, mathEnvOpts...)
	for _, id := range identifiers {
		opts = append(opts, cel.Variable(id, cel.DoubleType))
	}

	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("env: %w", err)
	}

	ast, iss := env.Parse(expr)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("parse: %w", iss.Err())
	}

	// Promote integer literals to doubles so they mix with the double-typed
	// variables (CEL has no double*int overload), then type-check the result.
	promoteIntLiteralsToDouble(ast.NativeRep().Expr())

	checked, iss := env.Check(ast)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("compile: %w", iss.Err())
	}

	prg, err := env.Program(checked)
	if err != nil {
		return nil, fmt.Errorf("program: %w", err)
	}

	return &cacheEntry{prg: prg, identifiers: identifiers}, nil
}

// promoteIntLiteralsToDouble rewrites every integer literal in the AST to the
// equivalent double literal, in place. This lets users write natural expressions
// like `tokens * 2` even though all variables are double-typed.
func promoteIntLiteralsToDouble(root celast.Expr) {
	fac := celast.NewExprFactory()
	celast.PostOrderVisit(root, celast.NewExprVisitor(func(e celast.Expr) {
		if e.Kind() != celast.LiteralKind {
			return
		}
		if iv, ok := e.AsLiteral().(types.Int); ok {
			e.SetKindCase(fac.NewLiteral(e.ID(), types.Double(float64(iv))))
		}
	}))
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

// buildActivation creates the activation map for the expression's identifiers.
// Only the identifiers referenced by the expression are populated (unused properties
// are ignored). Each referenced property is coerced to a float64 (matching the
// double-typed variables), so values arriving as strings (e.g. "2") participate in
// arithmetic. Missing identifiers are pre-filled with 0. A property that is present
// but not numeric is a hard error, since only numeric math is meaningful for a
// quantity (and would otherwise fail with an opaque CEL type error).
func (e *CELEvaluator) buildActivation(identifiers []string, properties map[string]any) (map[string]any, error) {
	activation := make(map[string]any, len(identifiers))
	for _, id := range identifiers {
		raw, ok := properties[id]
		if !ok {
			activation[id] = 0.0
			continue
		}
		f, ok := toFloat(raw)
		if !ok {
			return nil, fmt.Errorf("property %q is not a finite number (type %T)", id, raw)
		}
		activation[id] = f
	}
	return activation, nil
}

// toFloat coerces a property value to float64, including numeric strings like "2".
// Returns ok=false for values that cannot represent a finite number. NaN and
// ±Inf are rejected at this boundary so they can't reach decimal math
// (decimal.NewFromFloat panics on non-finite input, and downstream billing
// totals should never accept these as legitimate quantities).
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return finiteOk(n, nil)
	case float32:
		return finiteOk(float64(n), nil)
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return finiteOk(f, err)
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		return finiteOk(f, err)
	default:
		return 0, false
	}
}

// finiteOk returns (f, true) only if f is finite (not NaN, not ±Inf) and the
// parse/convert producing it succeeded. Used by toFloat to reject non-finite
// numeric inputs at the property boundary.
func finiteOk(f float64, err error) (float64, bool) {
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// toDecimal converts CEL result (from ref.Val.Value()) to decimal.Decimal.
func toDecimal(val any) (decimal.Decimal, error) {
	if val == nil {
		return decimal.Zero, fmt.Errorf("expression result is nil")
	}

	switch v := val.(type) {
	case float64:
		// decimal.NewFromFloat panics on ±Inf/NaN, which is exactly what a float
		// division-by-zero produces (e.g. a missing/zero divisor). Surface a clean
		// error instead of panicking the consumer/worker goroutine.
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return decimal.Zero, fmt.Errorf("expression produced a non-finite result (division by zero?)")
		}
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
