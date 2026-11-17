package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type testProvider string

type testCode int

type testLevel int

func (l testLevel) String() string { return "level-3" }

type testStruct struct{ A int }

func TestScalarCore_RewritesNamedScalars(t *testing.T) {
	inner, logs := observer.New(zapcore.DebugLevel)
	log := zap.New(newScalarCore(inner)).Sugar()

	log.Infow("msg",
		"provider", testProvider("quickbooks"),
		"level", testLevel(3),
		"code", testCode(42),
		"count", 7,
		"plain", "already-string",
		"obj", testStruct{A: 1},
	)

	require.Equal(t, 1, logs.Len())
	fields := map[string]zapcore.Field{}
	for _, f := range logs.All()[0].Context {
		fields[f.Key] = f
	}

	tests := []struct {
		key      string
		wantType zapcore.FieldType
	}{
		{"provider", zapcore.StringType},
		{"level", zapcore.StringerType},
		{"code", zapcore.Int64Type},
		{"count", zapcore.Int64Type},
		{"plain", zapcore.StringType},
		{"obj", zapcore.ReflectType},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			assert.Equal(t, tt.wantType, fields[tt.key].Type)
		})
	}
	assert.Equal(t, "quickbooks", fields["provider"].String)
	assert.Equal(t, int64(42), fields["code"].Integer)
}

func TestScalarCore_WithFieldsAndNoCallerMutation(t *testing.T) {
	inner, logs := observer.New(zapcore.DebugLevel)
	core := newScalarCore(inner).With([]zapcore.Field{zap.Any("provider", testProvider("stripe"))})

	orig := []zapcore.Field{zap.Any("p2", testProvider("razorpay"))}
	require.NoError(t, core.Write(zapcore.Entry{Message: "m"}, orig))

	assert.Equal(t, zapcore.ReflectType, orig[0].Type, "caller slice must not be mutated")
	ctx := logs.All()[0].Context
	require.Len(t, ctx, 2)
	for _, f := range ctx {
		assert.Equal(t, zapcore.StringType, f.Type, f.Key)
	}
}
