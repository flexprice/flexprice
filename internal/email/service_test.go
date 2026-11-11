package email

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestEmailService_LogsNoAddress(t *testing.T) {
	const to = "alice@acme.com"
	core, logs := observer.New(zapcore.DebugLevel)
	svc := NewEmail(NewEmailClient(Config{Enabled: false}), zap.New(core))

	_, _ = svc.SendEmail(context.Background(), SendEmailRequest{ToAddress: to, Subject: "s", Text: "t"})
	_, _ = svc.SendEmailWithTemplate(context.Background(), SendEmailWithTemplateRequest{ToAddress: to, Subject: "s", TemplatePath: "welcome-email.html"})

	if logs.Len() == 0 {
		t.Fatal("expected log entries")
	}
	for _, e := range logs.All() {
		if strings.Contains(e.Message, to) {
			t.Errorf("message leaks address: %s", e.Message)
		}
		for _, f := range e.Context {
			if strings.Contains(f.String, to) {
				t.Errorf("field %s leaks address", f.Key)
			}
		}
	}
}
