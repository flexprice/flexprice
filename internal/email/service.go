package email

import (
	"bytes"
	"context"
	"fmt"
	"html/template"

	"go.uber.org/zap"
)

// emailTemplates stores email templates as string constants
var emailTemplates = map[string]string{
	"welcome-email.html": `<!DOCTYPE html>
<html>
<head>
    <meta http-equiv="Content-Type" content="text/html; charset=UTF-8" />
    <title>Welcome to Flexprice</title>
</head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; font-size: 14px; line-height: 1.6; color: #333;">
    <p>Hey!</p>
    <p>This is Manish, founder of <a href="https://flexprice.io">Flexprice</a>.<br/>
    Thanks for signing up.</p>
    
    <p>We're building an open-source metering and billing platform for AI and agentic-based companies. We help you launch & iterate pricing without any dev bottlenecks.<br/>
    Please feel free to <strong>reply to this email</strong>, or you can book a quick 30-minute call with us: <a href="{{.calendar_url}}">{{.calendar_url}}</a></p>
    
    <p>Looking forward to hearing from you! 😊</p>
    
    <br/>
    <br/>
    
    <p>Best,<br/>
    Manish Choudhary<br/>
    Co-founder & CEO, Flexprice
    </p>
</body>
</html>`,
}

// EmailService handles email operations
type Email struct {
	client *EmailClient
	logger *zap.SugaredLogger
}

// NewEmailService creates a new email service
func NewEmail(client *EmailClient, logger *zap.Logger) *Email {
	return &Email{
		client: client,
		logger: logger.Sugar(),
	}
}

// SendEmail sends a plain text email
func (s *Email) SendEmail(ctx context.Context, req SendEmailRequest) (*SendEmailResponse, error) {
	if !s.client.IsEnabled() {
		s.logger.Info(ctx, "email client is disabled, skipping email send",
			"to", req.ToAddress,
			"subject", req.Subject,
		)
		return &SendEmailResponse{
			Success: false,
			Error:   "email client is disabled",
		}, nil
	}

	// Prioritize env var from address over request from address
	fromAddress := s.client.GetFromAddress()
	if fromAddress == "" {
		fromAddress = req.FromAddress
	}

	messageID, err := s.client.SendEmail(ctx, fromAddress, req.ToAddress, req.Subject, "", req.Text)
	if err != nil {
		s.logger.Error(ctx, "failed to send email",
			"error", err,
			"to", req.ToAddress,
			"subject", req.Subject,
		)
		return &SendEmailResponse{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	s.logger.Info(ctx, "email sent successfully",
		"message_id", messageID,
		"to", req.ToAddress,
		"subject", req.Subject,
	)

	return &SendEmailResponse{
		MessageID: messageID,
		Success:   true,
	}, nil
}

// SendEmailWithTemplate sends an email using an HTML template
func (s *Email) SendEmailWithTemplate(ctx context.Context, req SendEmailWithTemplateRequest) (*SendEmailWithTemplateResponse, error) {
	if !s.client.IsEnabled() {
		s.logger.Info(ctx, "email client is disabled, skipping email send",
			"to", req.ToAddress,
			"subject", req.Subject,
			"template", req.TemplatePath,
		)
		return &SendEmailWithTemplateResponse{
			Success: false,
			Error:   "email client is disabled",
		}, nil
	}

	// Prioritize env var from address over request from address
	fromAddress := s.client.GetFromAddress()
	if fromAddress == "" {
		fromAddress = req.FromAddress
	}

	s.logger.Debug(ctx, "preparing to send templated email",
		"from", fromAddress,
		"to", req.ToAddress,
		"subject", req.Subject,
		"template", req.TemplatePath,
	)

	// Retrieve the template from in-memory store
	htmlContent, err := s.readTemplate(req.TemplatePath)
	if err != nil {
		s.logger.Error(ctx, "failed to read email template",
			"error", err,
			"template", req.TemplatePath,
		)
		return &SendEmailWithTemplateResponse{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	s.logger.Debug(ctx, "template read successfully",
		"template", req.TemplatePath,
		"content_length", len(htmlContent),
	)

	// Render template with data using html/template
	htmlContent, err = s.renderTemplate(htmlContent, req.Data)
	if err != nil {
		s.logger.Error(ctx, "failed to render email template",
			"error", err,
			"template", req.TemplatePath,
		)
		return &SendEmailWithTemplateResponse{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	messageID, err := s.client.SendEmail(ctx, fromAddress, req.ToAddress, req.Subject, htmlContent, "")
	if err != nil {
		s.logger.Error(ctx, "failed to send templated email",
			"error", err,
			"from", fromAddress,
			"to", req.ToAddress,
			"subject", req.Subject,
			"template", req.TemplatePath,
		)
		return &SendEmailWithTemplateResponse{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	s.logger.Info(ctx, "templated email sent successfully",
		"message_id", messageID,
		"from", fromAddress,
		"to", req.ToAddress,
		"subject", req.Subject,
		"template", req.TemplatePath,
	)

	return &SendEmailWithTemplateResponse{
		MessageID: messageID,
		Success:   true,
	}, nil
}

func (s *Email) readTemplate(templatePath string) (string, error) {
	templateContent, exists := emailTemplates[templatePath]
	if !exists {
		return "", fmt.Errorf("template not found: %s", templatePath)
	}

	return templateContent, nil
}

// renderTemplate renders an HTML template using Go's html/template for safe HTML rendering
func (s *Email) renderTemplate(templateContent string, data map[string]interface{}) (string, error) {
	// Parse the template
	tmpl, err := template.New("email").Parse(templateContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Execute the template with data
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// BuildTemplateData builds template data from config values only
func BuildTemplateData(configData map[string]string, toAddress string) map[string]interface{} {
	data := make(map[string]interface{})

	// Add all config values
	for key, value := range configData {
		data[key] = value
	}

	return data
}
