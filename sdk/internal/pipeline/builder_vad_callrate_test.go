package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
)

// Voice shares the conversation's call-rate window, as text does, so calls in
// a voice session and approvals ResolveTool admits count in one window.
func TestVADProviderConfig_SharesCallRate(t *testing.T) {
	window := stage.NewCallRateWindow()
	assert.Same(t, window, vadProviderConfig(&Config{CallRate: window}).CallRate)
}
