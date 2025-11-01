package sdk

import (
	"errors"
	"fmt"
)

// Common error types for better error handling
var (
	ErrPackNotFound     = errors.New("pack not found")
	ErrPromptNotFound   = errors.New("prompt not found") 
	ErrInvalidConfig    = errors.New("invalid configuration")
	ErrProviderFailed   = errors.New("provider request failed")
	ErrValidationFailed = errors.New("validation failed")
)

// Error wrapping utilities for consistent error handling
func WrapPackError(err error, packPath string) error {
	return fmt.Errorf("pack error (%s): %w", packPath, err)
}

func WrapProviderError(err error, provider string) error {
	return fmt.Errorf("provider error (%s): %w", provider, err)
}

func WrapValidationError(err error, validator string) error {
	return fmt.Errorf("validation error (%s): %w", validator, err)
}

// IsTemporaryError checks if an error is temporary and should be retried
func IsTemporaryError(err error) bool {
	// Check for common temporary error patterns
	if errors.Is(err, ErrProviderFailed) {
		return true
	}
	// Add more patterns as needed
	return false
}

// IsRetryableError determines if an operation should be retried
func IsRetryableError(err error) bool {
	return IsTemporaryError(err)
}