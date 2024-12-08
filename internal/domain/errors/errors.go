// internal/domain/errors/errors.go
package errors

type AttributeNotFoundError struct {
	Attribute string
}

func (e *AttributeNotFoundError) Error() string {
	return "attribute not found: " + e.Attribute
}

type InvalidInputError struct {
	Input string
}

func (e *InvalidInputError) Error() string {
	return "invalid input: " + e.Input
}

func NewInvalidInputError(input string) *InvalidInputError {
	return &InvalidInputError{
		Input: input,
	}
}

func NewAttributeNotFoundError(attribute string) error {
	return &AttributeNotFoundError{
		Attribute: attribute,
	}
}
