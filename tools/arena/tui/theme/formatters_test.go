package theme

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatDuration(t *testing.T) {
	assert.Equal(t, "500ms", FormatDuration(500*time.Millisecond))
	assert.Equal(t, "2s", FormatDuration(2*time.Second))
	assert.Equal(t, "2.1s", FormatDuration(2*time.Second+145*time.Millisecond))
	assert.Equal(t, "2m30s", FormatDuration(2*time.Minute+30*time.Second))
}

func TestFormatCost(t *testing.T) {
	assert.Equal(t, "$0.0000", FormatCost(0))
	assert.Equal(t, "$0.0123", FormatCost(0.0123))
	assert.Equal(t, "$123.4567", FormatCost(123.4567))
}

func TestFormatNumber(t *testing.T) {
	assert.Equal(t, "123", FormatNumber(123))
	assert.Equal(t, "1,234", FormatNumber(1234))
	assert.Equal(t, "1,234,567", FormatNumber(1234567))
	assert.Equal(t, "0", FormatNumber(0))
}

func TestFormatPercentage(t *testing.T) {
	assert.Equal(t, "0%", FormatPercentage(5, 0))
	assert.Equal(t, "50.0%", FormatPercentage(5, 10))
	assert.Equal(t, "66.7%", FormatPercentage(2, 3))
	assert.Equal(t, "100.0%", FormatPercentage(10, 10))
}

func TestTruncateString(t *testing.T) {
	assert.Equal(t, "hello", TruncateString("hello", 10))
	assert.Equal(t, "hello", TruncateString("hello", 5))
	assert.Equal(t, "hello...", TruncateString("hello world", 8))
	assert.Equal(t, "...", TruncateString("hello", 2))
}

func TestCompactString(t *testing.T) {
	assert.Equal(t, "hello world", CompactString("hello\nworld"))
	assert.Equal(t, "hello world", CompactString("hello\tworld"))
	assert.Equal(t, "hello world", CompactString("hello    world"))
	assert.Equal(t, "hello world", CompactString("  hello\n\n  world\t\t  "))
}
