//go:build portaudio

// Package ui provides a terminal user interface for the voice interview.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/sdk/examples/voice-interview/interview"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			MarginBottom(1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	activeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")).
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFCC00"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4444"))

	scoreStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")).
			Bold(true)

	questionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Bold(true).
			MarginTop(1).
			MarginBottom(1)

	transcriptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Italic(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))
)

// Model represents the UI state
type Model struct {
	// Interview state
	state *interview.InterviewState

	// UI components
	progress   progress.Model
	spinner    spinner.Model
	audioLevel float64
	width      int
	height     int

	// Display state
	currentTranscript string
	lastAssistantText string
	speaking          bool
	webcamActive      bool
	error             string

	// Event channel
	events <-chan interview.Event
}

// Messages for tea.Cmd
type eventMsg interview.Event
type tickMsg time.Time

// NewModel creates a new UI model
func NewModel(state *interview.InterviewState, events <-chan interview.Event) Model {
	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	return Model{
		state:    state,
		progress: p,
		spinner:  s,
		events:   events,
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.listenForEvents(),
		tick(),
	)
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.progress.Width = msg.Width - 10

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case eventMsg:
		m = m.handleEvent(interview.Event(msg))
		cmds = append(cmds, m.listenForEvents())

	case tickMsg:
		cmds = append(cmds, tick())

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View implements tea.Model
func (m Model) View() string {
	var b strings.Builder

	// Header
	b.WriteString(titleStyle.Render("🎤 Voice Interview System"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(fmt.Sprintf("Topic: %s", m.state.Topic)))
	b.WriteString("\n\n")

	// Status section
	b.WriteString(m.renderStatus())
	b.WriteString("\n")

	// Progress section
	b.WriteString(m.renderProgress())
	b.WriteString("\n\n")

	// Audio visualization
	b.WriteString(m.renderAudioLevel())
	b.WriteString("\n\n")

	// Current question
	if q := m.state.GetCurrentQuestion(); q != nil {
		b.WriteString(boxStyle.Render(m.renderQuestion(q)))
		b.WriteString("\n\n")
	}

	// Transcript
	if m.currentTranscript != "" || m.lastAssistantText != "" {
		b.WriteString(m.renderTranscript())
		b.WriteString("\n\n")
	}

	// Error display
	if m.error != "" {
		b.WriteString(errorStyle.Render("⚠ " + m.error))
		b.WriteString("\n\n")
	}

	// Help
	b.WriteString(helpStyle.Render("Press 'q' to quit"))

	return b.String()
}

func (m Model) renderStatus() string {
	var parts []string

	// Mode indicator
	modeStr := m.state.Mode.String()
	parts = append(parts, statusStyle.Render("Mode: ")+activeStyle.Render(modeStr))

	// Status indicator
	status := m.state.GetStatus()
	statusStr := status.String()
	if status == interview.StatusInProgress || status == interview.StatusWaitingForResponse {
		parts = append(parts, m.spinner.View()+" "+activeStyle.Render(statusStr))
	} else {
		parts = append(parts, statusStyle.Render("Status: ")+statusStr)
	}

	// Webcam indicator
	if m.webcamActive {
		parts = append(parts, activeStyle.Render("📷 Webcam"))
	}

	// Duration
	duration := m.state.GetDuration()
	parts = append(parts, statusStyle.Render(fmt.Sprintf("Time: %s", formatDuration(duration))))

	return strings.Join(parts, "  │  ")
}

func (m Model) renderProgress() string {
	current := m.state.CurrentQuestion
	total := m.state.TotalQuestions
	score := m.state.GetTotalScore()
	maxScore := m.state.GetMaxScore()

	// Progress bar
	progressPercent := m.state.GetProgress() / 100.0
	progressBar := m.progress.ViewAs(progressPercent)

	// Score display
	scoreDisplay := scoreStyle.Render(fmt.Sprintf("Score: %d/%d", score, maxScore))

	// Question counter
	questionDisplay := fmt.Sprintf("Question %d of %d", current, total)

	return fmt.Sprintf("%s\n%s  │  %s", progressBar, questionDisplay, scoreDisplay)
}

func (m Model) renderAudioLevel() string {
	// Create audio level bar
	barWidth := 30
	filledWidth := int(m.audioLevel * float64(barWidth))
	if filledWidth > barWidth {
		filledWidth = barWidth
	}

	bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)

	indicator := "🔇"
	if m.speaking {
		indicator = "🎤"
	}

	levelStr := fmt.Sprintf("%.0f%%", m.audioLevel*100)

	return fmt.Sprintf("%s [%s] %s", indicator, bar, levelStr)
}

func (m Model) renderQuestion(q *interview.Question) string {
	var b strings.Builder

	b.WriteString(questionStyle.Render(fmt.Sprintf("Q%d: %s", m.state.CurrentQuestion, q.Text)))

	return b.String()
}

func (m Model) renderTranscript() string {
	var b strings.Builder

	if m.lastAssistantText != "" {
		b.WriteString("🤖 ")
		b.WriteString(transcriptStyle.Render(truncate(m.lastAssistantText, 200)))
	}

	if m.currentTranscript != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("👤 ")
		b.WriteString(transcriptStyle.Render(truncate(m.currentTranscript, 100)))
	}

	return b.String()
}

func (m Model) handleEvent(event interview.Event) Model {
	switch event.Type {
	case interview.EventUserSpeaking:
		m.speaking = true
		if level, ok := event.Data.(float64); ok {
			m.audioLevel = level
		}

	case interview.EventUserSilent:
		m.speaking = false
		if level, ok := event.Data.(float64); ok {
			m.audioLevel = level
		}

	case interview.EventTranscriptReceived:
		if text, ok := event.Data.(string); ok {
			m.lastAssistantText = text
		}

	case interview.EventError:
		if err, ok := event.Data.(error); ok {
			m.error = err.Error()
		}

	case interview.EventInterviewCompleted:
		m.lastAssistantText = "Interview completed! Check the summary below."
	}

	return m
}

func (m Model) listenForEvents() tea.Cmd {
	return func() tea.Msg {
		event := <-m.events
		return eventMsg(event)
	}
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	m := d / time.Minute
	s := (d % time.Minute) / time.Second
	return fmt.Sprintf("%02d:%02d", m, s)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// RunUI starts the terminal UI
func RunUI(state *interview.InterviewState, events <-chan interview.Event) error {
	p := tea.NewProgram(NewModel(state, events), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
