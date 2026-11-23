package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestIsNewMessage_HistorySourcesAreNotNew(t *testing.T) {
	cases := map[string]bool{
		"statestore":     false,
		"summary":        false,
		"retrieved":      false,
		"":               true,
		"recorded_input": true,
		"tool":           true,
	}
	for source, want := range cases {
		assert.Equal(t, want, IsNewMessage(&types.Message{Source: source}), "Source %q", source)
	}
}
