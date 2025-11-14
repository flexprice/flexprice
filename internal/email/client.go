package email

import (
	"context"
	"fmt"

	"github.com/resend/resend-go/v2"
)

// EmailClient represents an email client wrapper
type EmailClient struct {
	client      *resend.Client
	enabled     bool
	fromAddress string
	replyTo     string
}

// Config holds the email client configuration
type Config struct {
	Enabled     bool
	APIKey      string
	FromAddress string
	ReplyTo     string
}

// NewEmailClient creates a new email client
func NewEmailClient(cfg Config) *EmailClient {
	if !cfg.Enabled {
		return &EmailClient{
			enabled: false,
		}
	}

	if cfg.APIKey == "" {
		return &EmailClient{
			enabled: false,
		}
	}

	client := resend.NewClient(cfg.APIKey)

	return &EmailClient{
		client:      client,
		enabled:     true,
		fromAddress: cfg.FromAddress,
		replyTo:     cfg.ReplyTo,
	}
}

// IsEnabled returns whether the email client is enabled
func (c *EmailClient) IsEnabled() bool {
	return c.enabled
}

// GetFromAddress returns the default from address
func (c *EmailClient) GetFromAddress() string {
	return c.fromAddress
}

// GetReplyTo returns the default reply-to address
func (c *EmailClient) GetReplyTo() string {
	return c.replyTo
}

// SendEmail sends a plain text or HTML email
func (c *EmailClient) SendEmail(ctx context.Context, from, to, subject, htmlContent, textContent string) (string, error) {
	if !c.enabled {
		return "", fmt.Errorf("email client is disabled")
	}

	params := &resend.SendEmailRequest{
		From:    from,
		To:      []string{to},
		Subject: subject,
		Html:    htmlContent,
		Text:    textContent,
	}

	// Add reply-to if available
	if c.replyTo != "" {
		params.ReplyTo = c.replyTo
	}

	sent, err := c.client.Emails.SendWithContext(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to send email: %w", err)
	}

	return sent.Id, nil
}
