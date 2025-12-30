package email

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

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
		s.logger.Warnw("email client is disabled, skipping email send",
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
		s.logger.Errorw("failed to send email",
			"error", err,
			"to", req.ToAddress,
			"subject", req.Subject,
		)
		return &SendEmailResponse{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	s.logger.Infow("email sent successfully",
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
		s.logger.Warnw("email client is disabled, skipping email send",
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

	s.logger.Debugw("preparing to send templated email",
		"from", fromAddress,
		"to", req.ToAddress,
		"subject", req.Subject,
		"template", req.TemplatePath,
	)

	// Read the template file
	htmlContent, err := s.readTemplate(req.TemplatePath)
	if err != nil {
		s.logger.Errorw("failed to read email template",
			"error", err,
			"template", req.TemplatePath,
		)
		return &SendEmailWithTemplateResponse{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	s.logger.Debugw("template read successfully",
		"template", req.TemplatePath,
		"content_length", len(htmlContent),
	)

	// Render template with data using html/template
	htmlContent, err = s.renderTemplate(htmlContent, req.Data)
	if err != nil {
		s.logger.Errorw("failed to render email template",
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
		s.logger.Errorw("failed to send templated email",
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

	s.logger.Infow("templated email sent successfully",
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

// readTemplate reads an HTML template from the file system
func (s *Email) readTemplate(templatePath string) (string, error) {
	var fullPath string
	var err error

	// If the path is absolute, use it directly
	if filepath.IsAbs(templatePath) {
		fullPath = templatePath
	} else {
		// Try multiple possible locations for the template
		// 1. Docker container path: /app/assets/email-templates/
		// 2. Local development: relative to current working directory
		// 3. Relative to executable location

		possiblePaths := []string{
			filepath.Join("/app", "assets", "email-templates", templatePath), // Docker container
		}

		// Try current working directory (for local development)
		if cwd, err := os.Getwd(); err == nil {
			possiblePaths = append(possiblePaths, filepath.Join(cwd, "assets", "email-templates", templatePath))
		}

		// Try relative to executable location
		if execPath, err := os.Executable(); err == nil {
			execDir := filepath.Dir(execPath)
			possiblePaths = append(possiblePaths, filepath.Join(execDir, "assets", "email-templates", templatePath))
		}

		// Try each possible path until one exists
		found := false
		for _, path := range possiblePaths {
			if _, err := os.Stat(path); err == nil {
				fullPath = path
				found = true
				break
			}
		}

		if !found {
			return "", fmt.Errorf("template file not found in any of the expected locations: %v", possiblePaths)
		}
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read template file %s: %w", fullPath, err)
	}

	return string(content), nil
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
