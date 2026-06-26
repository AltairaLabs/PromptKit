package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestComposer_TextWrapsInput verifies the text composer renders the supplied
// input inside a box and is not in speech mode.
func TestComposer_TextWrapsInput(t *testing.T) {
	c := NewTextComposer()
	c.SetWidth(40)
	require.False(t, c.IsSpeech())
	out := c.View("> hello", false)
	require.Contains(t, out, "hello", "text composer should render the input")
}

// TestComposer_SpeechShowsMic verifies the speech composer renders the mic
// indicator and ignores the text input.
func TestComposer_SpeechShowsMic(t *testing.T) {
	c := NewSpeechComposer()
	c.SetWidth(40)
	require.True(t, c.IsSpeech())
	out := c.View("ignored input", false)
	require.Contains(t, out, "🎤")
	require.NotContains(t, out, "ignored input")
}

// TestComposer_TextFocusChangesBorder verifies focus selects a different border
// color (the rendered ANSI is environment-dependent, so assert the color).
func TestComposer_TextFocusChangesBorder(t *testing.T) {
	c := NewTextComposer()
	c.SetWidth(40)
	require.NotEqual(t, c.borderColor(true), c.borderColor(false))
}

// TestComposer_SetSpeechTogglesMode verifies SetSpeech switches modes.
func TestComposer_SetSpeechTogglesMode(t *testing.T) {
	c := NewTextComposer()
	require.False(t, c.IsSpeech())
	c.SetSpeech(true)
	require.True(t, c.IsSpeech())
	c.SetSpeech(false)
	require.False(t, c.IsSpeech())
}

// TestComposer_ZeroWidthSafe verifies rendering at width 0 does not panic.
func TestComposer_ZeroWidthSafe(t *testing.T) {
	c := NewTextComposer()
	require.NotPanics(t, func() { _ = c.View("x", true) })
	require.NotEmpty(t, strings.TrimSpace(NewSpeechComposer().View("", false)))
}
