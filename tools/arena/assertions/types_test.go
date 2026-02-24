package assertions

import (
	"testing"

	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/stretchr/testify/assert"
)

func TestAssertionConfig_ToValidatorConfig(t *testing.T) {
	cfg := AssertionConfig{
		Type:    "content_includes",
		Params:  map[string]interface{}{"patterns": []string{"hello"}},
		Message: "should include hello",
	}
	vc := cfg.ToValidatorConfig()
	assert.Equal(t, "content_includes", vc.Type)
	assert.Equal(t, cfg.Params, vc.Params)
}

func TestFromValidationResult(t *testing.T) {
	vr := runtimeValidators.ValidationResult{
		Passed:  true,
		Details: map[string]interface{}{"key": "value"},
	}
	ar := FromValidationResult(vr, "test message")
	assert.True(t, ar.Passed)
	assert.Equal(t, "test message", ar.Message)
	assert.Equal(t, vr.Details, ar.Details)
}
