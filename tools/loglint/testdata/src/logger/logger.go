package logger

import "context"

// ILogger is the minimal interface.
type ILogger interface {
	Debug(ctx context.Context, msg string, kv ...interface{})
	Info(ctx context.Context, msg string, kv ...interface{})
	Warn(ctx context.Context, msg string, kv ...interface{})
	Error(ctx context.Context, msg string, kv ...interface{})
	Fatal(ctx context.Context, msg string, kv ...interface{})
}

type zapLogger struct{}

func (l *zapLogger) Debug(_ context.Context, _ string, _ ...interface{}) {}
func (l *zapLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (l *zapLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (l *zapLogger) Error(_ context.Context, _ string, _ ...interface{}) {}
func (l *zapLogger) Fatal(_ context.Context, _ string, _ ...interface{}) {}

// Err produces structured fields for an error.
func Err(err error) []any {
	if err == nil {
		return []any{"error", "<nil>", "error_type", "<nil>"}
	}
	return []any{"error", err.Error(), "error_type", "<type>"}
}

// Op produces a structured "operation" field.
func Op(name string) []any {
	return []any{"operation", name}
}

// Event produces "entity", "action", and "operation" fields.
func Event(entity, action string) []any {
	return []any{"entity", entity, "action", action, "operation", entity + "." + action}
}

// Entity produces an "<entity>_id" field.
func Entity(name, id string) []any {
	return []any{name + "_id", id}
}
