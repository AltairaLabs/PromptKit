package web

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// init disables schema validation for tests so fixture configs that predate
// required schema fields (e.g. spec.description) still load cleanly.
func init() {
	if testing.Testing() {
		config.SchemaValidationDisabled.Store(true)
	}
}
