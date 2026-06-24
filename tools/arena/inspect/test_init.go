package inspect

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func init() {
	if testing.Testing() {
		config.SchemaValidationDisabled.Store(true)
	}
}
