package email

// SendEmailRequest represents a request to send a plain text email
// Example:
//
//	{
//		"from_address": "subrat@flexprice.io",
//		"to_address": "client@example.com",
//		"subject": "Welcome to Flexprice",
//		"text": "Hello, welcome to our platform!"
//	}
type SendEmailRequest struct {
	FromAddress string `json:"from_address" validate:"omitempty,email" example:"noreply@flexprice.io"`
	ToAddress   string `json:"to_address" validate:"required,email" example:"user@example.com"`
	Subject     string `json:"subject" validate:"required" example:"Welcome to Flexprice"`
	Text        string `json:"text" validate:"required" example:"Hello, welcome to our platform!"`
}

// SendEmailResponse represents the response from sending an email
type SendEmailResponse struct {
	MessageID string
	Success   bool
	Error     string
}

// SendEmailWithTemplateRequest represents a request to send an email with a template
// Data field is optional - if not provided or partially provided, values from config will be used
// Example:
//
//	{
//		"from_address": "subrat@flexprice.io",
//		"to_address": "client@example.com",
//		"subject": "Welcome to Flexprice!",
//		"template_path": "welcome-email.html",
//		"data": {
//			"calendar_url": "https://calendly.com/flexprice-30mins-chat/manish?month=2025-03"
//		}
//	}
type SendEmailWithTemplateRequest struct {
	FromAddress  string                 `json:"from_address" validate:"omitempty,email"`
	ToAddress    string                 `json:"to_address" validate:"required,email"`
	Subject      string                 `json:"subject" validate:"required"`
	TemplatePath string                 `json:"template_path" validate:"required"`
	Data         map[string]interface{} `json:"data" validate:"omitempty"`
}

// SendEmailWithTemplateResponse represents the response from sending a templated email
type SendEmailWithTemplateResponse struct {
	MessageID string
	Success   bool
	Error     string
}
