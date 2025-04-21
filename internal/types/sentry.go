package types

import "github.com/getsentry/sentry-go"

type SentryEvent struct {
	Message string
	Level   sentry.Level
	Extra   map[string]interface{}
}

type EventType string

const (
	EventTypeEventIngestion EventType = "event-ingestion"
)
