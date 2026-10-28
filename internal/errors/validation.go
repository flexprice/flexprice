package errors

// Validation builds a validation error whose message doubles as the user-facing hint.
func Validation(msg string) error {
	return NewError(msg).
		WithHint(msg).
		Mark(ErrValidation)
}

// Validationf is Validation with a formatted message.
func Validationf(format string, args ...any) error {
	return NewErrorf(format, args...).
		WithHintf(format, args...).
		Mark(ErrValidation)
}
