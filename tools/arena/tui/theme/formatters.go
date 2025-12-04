package theme

import (
	"fmt"
	"strings"
	"time"
)

const (
	numberSeparator     = 1000
	durationPrecisionMs = 100

	// PercentageMultiplier converts ratio to percentage
	PercentageMultiplier = 100
	// MinEllipsisLength is the minimum length to show ellipsis
	MinEllipsisLength = 3
	// ThousandSeparatorInterval is the digit count between thousand separators
	ThousandSeparatorInterval = 3
)

// FormatDuration formats a duration as a human-readable string (e.g., "500ms", "2s")
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	d = d.Round(durationPrecisionMs * time.Millisecond)
	return d.String()
}

// FormatCost formats a cost value as currency (e.g., "$0.0123")
func FormatCost(cost float64) string {
	return fmt.Sprintf("$%.4f", cost)
}

// FormatNumber formats a large number with thousand separators (e.g., "1,234,567")
func FormatNumber(n int64) string {
	if n < numberSeparator {
		return fmt.Sprintf("%d", n)
	}
	str := fmt.Sprintf("%d", n)
	var result strings.Builder
	for i, c := range str {
		if i > 0 && (len(str)-i)%ThousandSeparatorInterval == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}

// FormatPercentage formats a ratio as a percentage (e.g., "66.7%")
func FormatPercentage(numerator, denominator int) string {
	if denominator == 0 {
		return "0%"
	}
	pct := float64(numerator) / float64(denominator) * PercentageMultiplier
	return fmt.Sprintf("%.1f%%", pct)
}

// TruncateString truncates a string to maxLen characters with ellipsis
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= MinEllipsisLength {
		return "..."
	}
	return s[:maxLen-MinEllipsisLength] + "..."
}

// CompactString removes excess whitespace from a string
func CompactString(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
