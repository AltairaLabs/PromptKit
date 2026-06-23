package app

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// init disables schema validation when running tests so that unit tests do not
// make network requests to fetch remote JSON schemas.
func init() {
	if testing.Testing() {
		config.SchemaValidationDisabled.Store(true)
	}
}
