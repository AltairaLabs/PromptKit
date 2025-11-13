package turnexecutors

import (
	"fmt"
)

// MediaErrorType categorizes media loading errors
type MediaErrorType string

const (
	// MediaErrorTypeValidation indicates invalid media configuration
	MediaErrorTypeValidation MediaErrorType = "validation"
	// MediaErrorTypeNetwork indicates network/HTTP-related errors
	MediaErrorTypeNetwork MediaErrorType = "network"
	// MediaErrorTypeFile indicates file system errors
	MediaErrorTypeFile MediaErrorType = "file"
	// MediaErrorTypeSecurity indicates security-related errors
	MediaErrorTypeSecurity MediaErrorType = "security"
	// MediaErrorTypeSize indicates file size limit errors
	MediaErrorTypeSize MediaErrorType = "size"
	// MediaErrorTypeFormat indicates unsupported format errors
	MediaErrorTypeFormat MediaErrorType = "format"
)

// MediaLoadError represents a structured error for media loading operations
type MediaLoadError struct {
	Type        MediaErrorType // Error category
	Index       int            // Content part index where error occurred
	Source      string         // Media source (file path or URL)
	ContentType string         // Content type (image, audio, video)
	Message     string         // Human-readable error message
	Cause       error          // Underlying error (if any)
}

// Error implements the error interface
func (e *MediaLoadError) Error() string {
	if e.Source != "" {
		if e.Cause != nil {
			return fmt.Sprintf("%s error for %s at index %d (%s): %s: %v",
				e.Type, e.ContentType, e.Index, e.Source, e.Message, e.Cause)
		}
		return fmt.Sprintf("%s error for %s at index %d (%s): %s",
			e.Type, e.ContentType, e.Index, e.Source, e.Message)
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s error for %s at index %d: %s: %v",
			e.Type, e.ContentType, e.Index, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s error for %s at index %d: %s",
		e.Type, e.ContentType, e.Index, e.Message)
}

// Unwrap returns the underlying error
func (e *MediaLoadError) Unwrap() error {
	return e.Cause
}

// IsMediaLoadError checks if an error is a MediaLoadError
func IsMediaLoadError(err error) bool {
	_, ok := err.(*MediaLoadError)
	return ok
}

// NewValidationError creates a validation error
func NewValidationError(index int, contentType, source, message string) *MediaLoadError {
	return &MediaLoadError{
		Type:        MediaErrorTypeValidation,
		Index:       index,
		Source:      source,
		ContentType: contentType,
		Message:     message,
	}
}

// NewNetworkError creates a network error
func NewNetworkError(index int, contentType, source, message string, cause error) *MediaLoadError {
	return &MediaLoadError{
		Type:        MediaErrorTypeNetwork,
		Index:       index,
		Source:      source,
		ContentType: contentType,
		Message:     message,
		Cause:       cause,
	}
}

// NewFileError creates a file system error
func NewFileError(index int, contentType, source, message string, cause error) *MediaLoadError {
	return &MediaLoadError{
		Type:        MediaErrorTypeFile,
		Index:       index,
		Source:      source,
		ContentType: contentType,
		Message:     message,
		Cause:       cause,
	}
}

// NewSecurityError creates a security error
func NewSecurityError(index int, contentType, source, message string) *MediaLoadError {
	return &MediaLoadError{
		Type:        MediaErrorTypeSecurity,
		Index:       index,
		Source:      source,
		ContentType: contentType,
		Message:     message,
	}
}

// NewSizeError creates a file size error
func NewSizeError(index int, contentType, source, message string) *MediaLoadError {
	return &MediaLoadError{
		Type:        MediaErrorTypeSize,
		Index:       index,
		Source:      source,
		ContentType: contentType,
		Message:     message,
	}
}

// NewFormatError creates a format error
func NewFormatError(index int, contentType, source, message string) *MediaLoadError {
	return &MediaLoadError{
		Type:        MediaErrorTypeFormat,
		Index:       index,
		Source:      source,
		ContentType: contentType,
		Message:     message,
	}
}
