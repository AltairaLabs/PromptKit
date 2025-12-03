package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestBorderColorFunctions(t *testing.T) {
	assert.Equal(t, lipgloss.Color(ColorIndigo), BorderColorFocused())
	assert.Equal(t, lipgloss.Color(ColorGray), BorderColorUnfocused())
	assert.Equal(t, lipgloss.Color(ColorSuccess), BorderColorSuccess())
	assert.Equal(t, lipgloss.Color(ColorError), BorderColorError())
}

func TestStylesAreDefined(t *testing.T) {
	assert.NotNil(t, TitleStyle)
	assert.NotNil(t, SuccessStyle)
	assert.NotNil(t, ErrorStyle)
	assert.NotNil(t, WarningStyle)
	assert.NotNil(t, InfoStyle)
	assert.NotNil(t, LabelStyle)
	assert.NotNil(t, ValueStyle)
	assert.NotNil(t, BorderedBoxStyle)
	assert.NotNil(t, SubtleTextStyle)
}
