package theme

import "github.com/charmbracelet/lipgloss"

const (
	// BoxPaddingHorizontal is the horizontal padding for bordered boxes
	BoxPaddingHorizontal = 2
	// BoxPaddingVertical is the vertical padding for bordered boxes
	BoxPaddingVertical = 1
)

var (
	// TitleStyle is used for section titles
	TitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorPrimary))
	// SuccessStyle indicates successful operations
	SuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSuccess)).Bold(true)
	// ErrorStyle indicates error states
	ErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorError)).Bold(true)
	// WarningStyle indicates warning states
	WarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWarning)).Bold(true)
	// InfoStyle indicates informational states
	InfoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorInfo))
	// LabelStyle is used for labels
	LabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorLightGray))
	// ValueStyle is used for values
	ValueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWhite)).Bold(true)
	// BorderedBoxStyle is a box with rounded borders and padding
	BorderedBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				Padding(BoxPaddingVertical, BoxPaddingHorizontal)
	// SubtleTextStyle is used for less prominent text
	SubtleTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray))
)

// BorderColorFocused returns the border color for focused elements
func BorderColorFocused() lipgloss.Color { return lipgloss.Color(ColorIndigo) }

// BorderColorUnfocused returns the border color for unfocused elements
func BorderColorUnfocused() lipgloss.Color { return lipgloss.Color(ColorGray) }

// BorderColorSuccess returns the border color for success states
func BorderColorSuccess() lipgloss.Color { return lipgloss.Color(ColorSuccess) }

// BorderColorError returns the border color for error states
func BorderColorError() lipgloss.Color { return lipgloss.Color(ColorError) }
