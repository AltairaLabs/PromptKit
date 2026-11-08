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

	// ErrFragmentPathUnsupported is returned by LoadFragment from a repository
	// that cannot load a fragment by path, such as the in-memory one, which
	// holds fragments by name only. It does not satisfy fs.ErrNotExist: the
	// fragment may exist, and the repository cannot tell, so FragmentResolver
	// reports it rather than looking for the path elsewhere.
	ErrFragmentPathUnsupported = errors.New("this repository cannot load a fragment by path")

	// ErrFragmentPathOutsideBase is returned by LoadFragment for a relative
	// fragment path that resolves outside the repository's base directory. It
	// does not satisfy fs.ErrNotExist, so FragmentResolver does not look for
	// the path elsewhere. A lookup with no base dir (the resolver's
	// working-directory fallback) that leaves the working directory also
	// satisfies fs.ErrNotExist. An absolute path written in the config is not
	// affected.
	ErrFragmentPathOutsideBase = errors.New("fragment path resolves outside the repository's base directory")
)
