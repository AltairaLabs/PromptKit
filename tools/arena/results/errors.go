package results

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// Error message templates for consistent formatting
	repositoryFailedTemplate = "repository %d failed: %w"
)

// CompositeError represents multiple errors from repository operations
type CompositeError struct {
	Operation string
	Errors    []error
}

// NewCompositeError creates a new composite error
func NewCompositeError(operation string, errs []error) *CompositeError {
	return &CompositeError{
		Operation: operation,
		Errors:    errs,
	}
}

// Error implements the error interface
func (e *CompositeError) Error() string {
	if len(e.Errors) == 0 {
		return fmt.Sprintf("%s: no errors", e.Operation)
	}

	if len(e.Errors) == 1 {
		return fmt.Sprintf("%s: %v", e.Operation, e.Errors[0])
	}

	var msgs []string
	for _, err := range e.Errors {
		msgs = append(msgs, err.Error())
	}

	return fmt.Sprintf("%s: multiple errors: [%s]", e.Operation, strings.Join(msgs, "; "))
}

// Unwrap returns the first error for compatibility with errors.Unwrap
func (e *CompositeError) Unwrap() error {
	if len(e.Errors) == 0 {
		return nil
	}
	return e.Errors[0]
}

// UnsupportedOperationError represents an operation not supported by a repository
type UnsupportedOperationError struct {
	Operation string
	Reason    string
}

// NewUnsupportedOperationError creates a new unsupported operation error
func NewUnsupportedOperationError(operation, reason string) *UnsupportedOperationError {
	return &UnsupportedOperationError{
		Operation: operation,
		Reason:    reason,
	}
}

// Error implements the error interface
func (e *UnsupportedOperationError) Error() string {
	return fmt.Sprintf("operation %s not supported: %s", e.Operation, e.Reason)
}

// IsUnsupportedOperation checks if an error is an UnsupportedOperationError
func IsUnsupportedOperation(err error) bool {
	var unsupportedErr *UnsupportedOperationError
	return errors.As(err, &unsupportedErr)
}

// ValidationError represents a validation failure in result processing
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

// NewValidationError creates a new validation error
func NewValidationError(field string, value interface{}, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	}
}

// Error implements the error interface
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for field %s (value: %v): %s", e.Field, e.Value, e.Message)
}
