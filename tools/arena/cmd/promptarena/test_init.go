package main

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// init is called before any tests run in this package
// Disable schema validation for tests since schemas may not be published yet
func init() {
	// Disable schema validation when running tests
	if testing.Testing() {
		config.SchemaValidationEnabled = false
	}
}
