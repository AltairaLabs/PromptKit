package inspect

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

const (
	boxWidth = 78
	// boxChrome is the horizontal space a box's border (2) + padding (2) adds on
	// top of its content width.
	boxChrome = 4
	// minBoxWidth keeps boxes readable on very narrow terminals.
	minBoxWidth = 40
	// minColWidth is the narrowest a grid column (box content + chrome) may be;
	// it sets how soon extra columns are added as the terminal widens.
	minColWidth = 46
	// colGap is the blank space between grid columns.
	colGap = 2
	// maxColumns caps the grid so boxes never get too thin to read.
	maxColumns          = 3
	maxDescLength       = 55
	maxDescLengthPrompt = 60
	maxGoalsDisplayed   = 3
	maxGoalLength       = 50

	// Label constants for display output
	labelFile        = "  File: "
	labelDesc        = "  Desc: "
	labelModel       = "  Model: "
	labelTemperature = "  Temperature: "
	labelMaxTok      = "  Max Tokens: "
	labelTaskType    = "  Task Type: "
	labelTask        = "  Task: "
	labelVersion     = "  Version: "
	labelVariables   = "  Variables: "
	labelTools       = "  Tools: "
	labelValidators  = "  Validators: "
	labelMedia       = "  Media: "
	labelMode        = "  Mode: "
	labelParams      = "  Params: "
	labelTimeout     = "  Timeout: "
	labelFeatures    = "  Features: "
	labelFlags       = "  Flags: "
	labelProviders   = "  Providers: "
	labelGoals       = "  Goals:"

	// Section names
	sectionValidation = "validation"
)

// Style definitions for terminal output
var (
	// Box style — width is applied per render (full width or a grid column).
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(theme.ColorPrimary)).
			Padding(0, 1)

	sectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(theme.ColorWhite)).
				Background(lipgloss.Color(theme.ColorPrimary)).
				Padding(0, 1).
				MarginTop(1)

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorLightGray))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorWhite))

	highlightStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorEmerald)).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorGray))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorSuccess))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorWarning))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorError))

	tagStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorPrimary)).
			Background(lipgloss.Color("#2D2D3D")).
			Padding(0, 1)
)

// RenderText renders all inspection sections to a string.
// opts controls which sections and detail levels are included.
// It captures stdout so that the lipgloss Print calls are collected into
// the returned string.
// Grid layout state for the current render. The renderer is single-threaded (it
// captures global stdout), so per-call package state is safe.
var (
	fullBoxWidth  = boxWidth
	colBoxWidth   = boxWidth
	activeColumns = 1
)

// effectiveBoxWidth returns the full content width for boxes: the terminal width
// minus the box border/padding when a width is supplied (TUI), else the default
// fixed print width (CLI).
func effectiveBoxWidth(termWidth int) int {
	if termWidth <= 0 {
		return boxWidth
	}
	if w := termWidth - boxChrome; w > minBoxWidth {
		return w
	}
	return minBoxWidth
}

// computeColumns returns how many grid columns fit in termWidth, capped at
// maxColumns. Zero/negative width (CLI) renders a single column.
func computeColumns(termWidth int) int {
	if termWidth <= 0 {
		return 1
	}
	cols := (termWidth + colGap) / (minColWidth + colGap)
	if cols < 1 {
		cols = 1
	}
	if cols > maxColumns {
		cols = maxColumns
	}
	return cols
}

// columnWidth returns the content width of a single box in a cols-wide grid.
func columnWidth(termWidth, cols int) int {
	if termWidth <= 0 || cols <= 1 {
		return effectiveBoxWidth(termWidth)
	}
	avail := termWidth - colGap*(cols-1)
	if w := avail/cols - boxChrome; w > minBoxWidth {
		return w
	}
	return minBoxWidth
}

// printBoxes renders each line set as a box, stacked vertically. Boxes take the
// full width in single-column layout and a column width when the sections are
// laid out as a grid (so each section fits within its column).
func printBoxes(lineSets [][]string) {
	width := fullBoxWidth
	if activeColumns > 1 {
		width = colBoxWidth
	}
	for _, ls := range lineSets {
		fmt.Println(boxStyle.Width(width).Render(strings.Join(ls, "\n")))
	}
}

// captureStdout runs fn with stdout redirected to a pipe and returns everything
// it wrote. Used to render each section into a self-contained block so the
// blocks can be arranged into columns. Nests safely inside RenderText's own
// capture (it saves and restores whatever stdout it found).
func captureStdout(fn func()) string {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		fn()
		return ""
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String()
}

// emitSectionBlocks arranges whole section blocks into the active number of
// columns using shortest-column-first packing (a masonry/dashboard layout), so
// even configs with one item per section fill a wide terminal. A single-column
// layout just stacks them.
func emitSectionBlocks(blocks []string) {
	if len(blocks) == 0 {
		return
	}
	if activeColumns <= 1 {
		fmt.Println(strings.Join(blocks, "\n\n"))
		return
	}
	columns := make([]string, activeColumns)
	heights := make([]int, activeColumns)
	for _, b := range blocks {
		shortest := 0
		for i := 1; i < activeColumns; i++ {
			if heights[i] < heights[shortest] {
				shortest = i
			}
		}
		if columns[shortest] != "" {
			columns[shortest] += "\n\n"
		}
		columns[shortest] += b
		heights[shortest] += lipgloss.Height(b) + 1
	}
	gap := strings.Repeat(" ", colGap)
	cells := make([]string, 0, activeColumns*2-1)
	for i, c := range columns {
		if i > 0 {
			cells = append(cells, gap)
		}
		cells = append(cells, c)
	}
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Top, cells...))
}

func RenderText(data *InspectionData, opts RenderOptions) string {
	// Size the grid for this render.
	fullBoxWidth = effectiveBoxWidth(opts.Width)
	activeColumns = computeColumns(opts.Width)
	colBoxWidth = columnWidth(opts.Width, activeColumns)

	// Capture everything written to stdout during rendering.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		// Fallback: render directly to stdout and return empty string.
		outputText(data, opts)
		return ""
	}
	os.Stdout = w

	outputText(data, opts)

	// Restore stdout and collect output.
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()

	return buf.String()
}

func outputText(data *InspectionData, opts RenderOptions) {
	printConfigHeader(data.ConfigFile)
	printSections(data, opts)
}

// printConfigHeader prints the inspected config's path. The "PromptArena" banner
// and "Inspect" title are supplied by the surrounding chrome, so no title is
// repeated here.
func printConfigHeader(configFile string) {
	fmt.Println(boxStyle.Width(fullBoxWidth).Render(
		labelStyle.Render("Configuration: ") + highlightStyle.Render(configFile),
	))
	fmt.Println()
}

// getSectionVisibility determines which sections to show.
// Pass an empty string to show all sections.
func getSectionVisibility(section string) SectionVisibility {
	showAll := section == ""
	return SectionVisibility{
		All:        showAll,
		Prompts:    showAll || section == "prompts",
		Providers:  showAll || section == "providers",
		Scenarios:  showAll || section == "scenarios",
		Tools:      showAll || section == "tools",
		Selfplay:   showAll || section == "selfplay" || section == "personas",
		Judges:     showAll || section == "judges",
		Defaults:   showAll || section == "defaults",
		Validation: showAll || section == sectionValidation,
	}
}

// printSections prints all visible sections
// printSections renders every visible section into its own block, then arranges
// the blocks into columns so the inspector fills a wide terminal.
func printSections(data *InspectionData, opts RenderOptions) {
	vis := getSectionVisibility(opts.Section)
	candidates := []struct {
		show bool
		fn   func()
	}{
		{vis.Prompts && len(data.PromptConfigs) > 0, func() { printPromptsSection(data, opts) }},
		{vis.Providers && len(data.Providers) > 0, func() { printProvidersSection(data, opts) }},
		{vis.Scenarios && len(data.Scenarios) > 0, func() { printScenariosSection(data, opts) }},
		{vis.Tools && len(data.Tools) > 0, func() { printToolsSection(data, opts) }},
		{
			vis.Selfplay && (len(data.Personas) > 0 || len(data.SelfPlayRoles) > 0),
			func() { printPersonasSection(data, opts) },
		},
		{vis.Judges && len(data.Judges) > 0, func() { printJudgesSection(data) }},
		{vis.Defaults && data.Defaults != nil, func() { printDefaultsSection(data, opts) }},
		{vis.Validation, func() { printValidationSection(data) }},
		{opts.Stats && data.CacheStats != nil, func() { printCacheStatistics(data.CacheStats) }},
	}

	var blocks []string
	for _, c := range candidates {
		if !c.show {
			continue
		}
		if block := strings.TrimRight(captureStdout(c.fn), "\n"); strings.TrimSpace(block) != "" {
			blocks = append(blocks, block)
		}
	}
	emitSectionBlocks(blocks)
}

// truncateInspectString truncates a string to the given length with ellipsis
func truncateInspectString(s string, maxLen int) string {
	// Remove newlines and extra whitespace
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
