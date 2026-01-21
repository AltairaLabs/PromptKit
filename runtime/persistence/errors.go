// Package persistence provides persistence interfaces and common errors.
package persistence

import "errors"

// Sentinel errors for persistence operations.
var (
	// ErrNilConfig is returned when a nil config is passed to SavePrompt.
	ErrNilConfig = errors.New("config cannot be nil")

	// ErrNilDescriptor is returned when a nil descriptor is passed to SaveTool.
	ErrNilDescriptor = errors.New("descriptor cannot be nil")

	// ErrEmptyTaskType is returned when a config has an empty task_type.
	ErrEmptyTaskType = errors.New("task_type cannot be empty")

	// ErrEmptyToolName is returned when a tool descriptor has an empty name.
	ErrEmptyToolName = errors.New("tool name cannot be empty")

	// ErrPromptNotFound is returned when a requested prompt is not found.
	ErrPromptNotFound = errors.New("prompt not found")

	// ErrToolNotFound is returned when a requested tool is not found.
	ErrToolNotFound = errors.New("tool not found")
)
