// Package tui provides a terminal user interface for PromptArena execution monitoring.
// It implements a multi-pane display showing active runs, metrics, and logs in real-time.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/viewmodels"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

// Terminal size requirements
const (
	MinTerminalWidth  = 80
	MinTerminalHeight = 24
)

// Display constants
const (
	maxLogBufferSize    = 100
	tickIntervalMs      = 500
	bannerLines         = 2  // Banner + info line (separator counted separately)
	bottomPanelPadding  = 4  // Border + padding
	metricsBoxWidth     = 40 // Fixed width for metrics box
	summaryWidthDivisor = 2
	summaryMinWidth     = 40
)

// Color constants
type pane int

const (
	paneRuns pane = iota
	paneLogs
	paneResult
)

type page int

const (
	pageMain page = iota
	pageConversation
)

// runResultStorer is a minimal interface to retrieve run results from the state store.
type runResultStorer interface {
	GetResult(ctx context.Context, runID string) (*statestore.RunResult, error)
}

// Model represents the bubbletea application state
type Model struct {
	mu sync.Mutex

	width  int
	height int

	configFile     string
	totalRuns      int
	startTime      time.Time
	activeRuns     []RunInfo
	completedCount int
	successCount   int
	failedCount    int
	totalCost      float64
	totalDuration  time.Duration

	logs []LogEntry

	isTUIMode      bool
	fallbackReason string

	stateStore runResultStorer
	ctx        context.Context // Context for statestore operations

	activePane pane

	// Page components
	mainPage         *pages.MainPage
	conversationPage *pages.ConversationPage

	currentPage page

	// Cache of system prompts by conversation ID for when user navigates to conversation page
	systemPrompts map[string]string

	// Cache of messages by conversation ID for running conversations
	// This allows real-time message accumulation regardless of which page is displayed
	conversationMessages map[string][]types.Message
}

// RunInfo tracks information about a single run
type RunInfo struct {
	RunID            string
	Scenario         string
	Provider         string
	Region           string
	Status           RunStatus
	Duration         time.Duration
	Cost             float64
	Error            string
	StartTime        time.Time
	CurrentTurnIndex int
	CurrentTurnRole  string
	Selected         bool
}

// RunStatus represents the current state of a run
type RunStatus int

const (
	// StatusRunning indicates the run is currently executing
	StatusRunning RunStatus = iota
	// StatusCompleted indicates the run completed successfully
	StatusCompleted
	// StatusFailed indicates the run failed with an error
	StatusFailed
)

// LogEntry represents a single log line
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
}

type tickMsg time.Time

// Init initializes the bubbletea model
func (m *Model) Init() tea.Cmd {
	return tick()
}

// Update processes bubbletea messages and updates the model state
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.mu.Lock()
		m.width = msg.Width
		m.height = msg.Height
		m.mu.Unlock()
		return m, nil

	case tickMsg:
		return m, tick()

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.MouseMsg:
		// Mouse handling can be added to panels if needed
		return m, nil

	case RunStartedMsg:
		m.mu.Lock()
		m.handleRunStarted(&msg)
		m.mu.Unlock()
		return m, nil

	case RunCompletedMsg:
		m.mu.Lock()
		m.handleRunCompleted(&msg)
		m.mu.Unlock()
		return m, nil

	case RunFailedMsg:
		m.mu.Lock()
		m.handleRunFailed(&msg)
		m.mu.Unlock()
		return m, nil

	case TurnStartedMsg:
		m.mu.Lock()
		m.handleTurnStarted(&msg)
		m.mu.Unlock()
		return m, nil

	case TurnCompletedMsg:
		m.mu.Lock()
		m.handleTurnCompleted(&msg)
		m.mu.Unlock()
		return m, nil

	case MessageCreatedMsg:
		m.mu.Lock()
		m.handleMessageCreated(&msg)
		m.mu.Unlock()
		return m, nil

	case MessageUpdatedMsg:
		m.mu.Lock()
		m.handleMessageUpdated(&msg)
		m.mu.Unlock()
		return m, nil

	case ConversationStartedMsg:
		m.mu.Lock()
		m.handleConversationStarted(&msg)
		m.mu.Unlock()
		return m, nil

	case logging.Msg:
		m.mu.Lock()
		m.handleLogMsg(&msg)
		m.mu.Unlock()
		return m, nil
	}

	return m, nil
}

// handleRunStarted processes a run started event from the observer.
// Note: Called with mutex already locked by Update.
func (m *Model) handleRunStarted(msg *RunStartedMsg) {
	// Add to active runs
	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     msg.RunID,
		Scenario:  msg.Scenario,
		Provider:  msg.Provider,
		Region:    msg.Region,
		Status:    StatusRunning,
		StartTime: msg.Time,
	})

	// Log the event
	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Time,
		Level:     "INFO",
		Message:   fmt.Sprintf("Started: %s/%s/%s", msg.Provider, msg.Scenario, msg.Region),
	})
	m.trimLogs()
}

// handleRunCompleted processes a run completed event from the observer.
// Note: Called with mutex already locked by Update.
func (m *Model) handleRunCompleted(msg *RunCompletedMsg) {
	// Find and update the run in activeRuns
	for i := range m.activeRuns {
		if m.activeRuns[i].RunID != msg.RunID {
			continue
		}
		m.activeRuns[i].Status = StatusCompleted
		m.activeRuns[i].Duration = msg.Duration
		m.activeRuns[i].Cost = msg.Cost
		m.activeRuns[i].CurrentTurnRole = ""
		m.activeRuns[i].CurrentTurnIndex = 0
		break
	}

	// Update metrics
	m.completedCount++
	m.successCount++
	m.totalDuration += msg.Duration
	m.totalCost += msg.Cost

	// Log the event
	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Time,
		Level:     "INFO",
		Message:   fmt.Sprintf("Completed: %s (%.1fs, $%.4f)", msg.RunID, msg.Duration.Seconds(), msg.Cost),
	})
	m.trimLogs()

	// Clean up cached messages for this run (state store now has the data)
	if m.conversationMessages != nil {
		delete(m.conversationMessages, msg.RunID)
	}
	// Also clean up system prompt cache
	if m.systemPrompts != nil {
		delete(m.systemPrompts, msg.RunID)
	}

	// Remove from active runs after a brief display (keep for visual feedback)
}

// handleRunFailed processes a run failed event from the observer.
// Note: Called with mutex already locked by Update.
func (m *Model) handleRunFailed(msg *RunFailedMsg) {
	// Find and update the run in activeRuns
	for i := range m.activeRuns {
		if m.activeRuns[i].RunID != msg.RunID {
			continue
		}
		m.activeRuns[i].Status = StatusFailed
		m.activeRuns[i].Error = msg.Error.Error()
		m.activeRuns[i].CurrentTurnRole = ""
		m.activeRuns[i].CurrentTurnIndex = 0
		break
	}

	// Update metrics
	m.completedCount++
	m.failedCount++

	// Log the event
	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Time,
		Level:     "ERROR",
		Message:   fmt.Sprintf("Failed: %s - %v", msg.RunID, msg.Error),
	})
	m.trimLogs()

	// Clean up cached messages for this run
	if m.conversationMessages != nil {
		delete(m.conversationMessages, msg.RunID)
	}
	if m.systemPrompts != nil {
		delete(m.systemPrompts, msg.RunID)
	}
}

// handleTurnStarted updates the run with turn information.
func (m *Model) handleTurnStarted(msg *TurnStartedMsg) {
	for i := range m.activeRuns {
		if m.activeRuns[i].RunID == msg.RunID {
			m.activeRuns[i].CurrentTurnRole = msg.Role
			m.activeRuns[i].CurrentTurnIndex = msg.TurnIndex
			break
		}
	}

	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Time,
		Level:     "INFO",
		Message:   fmt.Sprintf("Turn %d (%s) started: %s", msg.TurnIndex+1, msg.Role, msg.RunID),
	})
	m.trimLogs()
}

// handleTurnCompleted updates the run when a turn finishes.
func (m *Model) handleTurnCompleted(msg *TurnCompletedMsg) {
	for i := range m.activeRuns {
		if m.activeRuns[i].RunID == msg.RunID {
			m.activeRuns[i].CurrentTurnRole = msg.Role
			m.activeRuns[i].CurrentTurnIndex = msg.TurnIndex
			if msg.Error != nil {
				m.activeRuns[i].Error = msg.Error.Error()
			}
			break
		}
	}

	level := "INFO"
	text := fmt.Sprintf("Turn %d (%s) completed: %s", msg.TurnIndex+1, msg.Role, msg.RunID)
	if msg.Error != nil {
		level = "ERROR"
		text = fmt.Sprintf("Turn %d (%s) failed: %s - %v", msg.TurnIndex+1, msg.Role, msg.RunID, msg.Error)
	}

	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Time,
		Level:     level,
		Message:   text,
	})
	m.trimLogs()
}

// handleMessageCreated processes a message.created event for real-time conversation updates
func (m *Model) handleMessageCreated(msg *MessageCreatedMsg) {
	logger.Debug("MessageCreatedMsg received",
		"conversation_id", msg.ConversationID,
		"role", msg.Role,
		"index", msg.Index,
		"current_page", m.currentPage)

	// Convert message data to types.Message
	var toolCalls []types.MessageToolCall
	for _, tc := range msg.ToolCalls {
		toolCalls = append(toolCalls, types.MessageToolCall{
			ID:   tc.ID,
			Name: tc.Name,
			Args: json.RawMessage(tc.Args),
		})
	}
	var toolResult *types.MessageToolResult
	if msg.ToolResult != nil {
		toolResult = &types.MessageToolResult{
			ID:        msg.ToolResult.ID,
			Name:      msg.ToolResult.Name,
			Content:   msg.ToolResult.Content,
			Error:     msg.ToolResult.Error,
			LatencyMs: msg.ToolResult.LatencyMs,
		}
	}
	newMsg := types.Message{
		Role:       msg.Role,
		Content:    msg.Content,
		Timestamp:  msg.Time,
		ToolCalls:  toolCalls,
		ToolResult: toolResult,
	}

	// Always cache the message for this conversation (regardless of current page/selection)
	if m.conversationMessages == nil {
		m.conversationMessages = make(map[string][]types.Message)
	}
	m.conversationMessages[msg.ConversationID] = append(m.conversationMessages[msg.ConversationID], newMsg)
	logger.Debug("Message cached",
		"conversation_id", msg.ConversationID,
		"total_cached", len(m.conversationMessages[msg.ConversationID]))

	// Also update the panel if we're viewing this conversation
	if m.currentPage == pageConversation {
		selected := m.selectedRun()
		if selected != nil && selected.RunID == msg.ConversationID {
			if m.conversationPage != nil {
				panel := m.conversationPage.Panel()
				if panel != nil {
					panel.AppendMessage(&newMsg)
					logger.Debug("Message appended to conversation panel",
						"tool_calls", len(toolCalls),
						"has_tool_result", toolResult != nil)
				}
			}
		}
	}
}

// handleConversationStarted processes a conversation.started event to set the system prompt
func (m *Model) handleConversationStarted(msg *ConversationStartedMsg) {
	logger.Debug("ConversationStartedMsg received",
		"conversation_id", msg.ConversationID)

	// Store the system prompt for this conversation so it can be displayed
	// when the user navigates to the conversation page
	if m.conversationPage != nil {
		panel := m.conversationPage.Panel()
		if panel != nil && !panel.HasSystemPrompt() {
			// Check if this is for the currently selected run
			selected := m.selectedRun()
			if selected != nil && selected.RunID == msg.ConversationID {
				newMsg := types.Message{
					Role:      "system",
					Content:   msg.SystemPrompt,
					Timestamp: msg.Time,
				}
				panel.PrependSystemPrompt(&newMsg)
				logger.Debug("System prompt set from conversation.started event")
			}
		}
	}

	// Also store in a map for later use when user navigates to conversation page
	if m.systemPrompts == nil {
		m.systemPrompts = make(map[string]string)
	}
	m.systemPrompts[msg.ConversationID] = msg.SystemPrompt
}

// handleMessageUpdated processes a message.updated event for cost/latency updates
func (m *Model) handleMessageUpdated(msg *MessageUpdatedMsg) {
	logger.Debug("MessageUpdatedMsg received",
		"conversation_id", msg.ConversationID,
		"index", msg.Index)

	// Only update if we're viewing the conversation page and the message is for the selected run
	if m.currentPage != pageConversation {
		return
	}

	selected := m.selectedRun()
	if selected == nil || selected.RunID != msg.ConversationID {
		return
	}

	if m.conversationPage != nil {
		panel := m.conversationPage.Panel()
		if panel != nil {
			costInfo := types.CostInfo{
				InputTokens:  msg.InputTokens,
				OutputTokens: msg.OutputTokens,
				TotalCost:    msg.TotalCost,
			}
			panel.UpdateMessageMetadata(msg.Index, msg.LatencyMs, costInfo)
		}
	}
}

// handleLogMsg processes a log message from the interceptor
func (m *Model) handleLogMsg(msg *logging.Msg) {
	m.logs = append(m.logs, LogEntry{
		Timestamp: msg.Timestamp,
		Level:     msg.Level,
		Message:   msg.Message,
	})
	m.trimLogs()

	// Auto-scroll handled by LogsPanel
	if m.mainPage != nil {
		m.mainPage.LogsPanel().GotoBottom()
	}
}

// trimLogs keeps the log buffer size within limits.
func (m *Model) trimLogs() {
	if len(m.logs) > maxLogBufferSize {
		m.logs = m.logs[len(m.logs)-maxLogBufferSize:]
	}
}

// View renders the TUI
func (m *Model) View() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isTUIMode {
		return ""
	}

	elapsed := time.Since(m.startTime).Truncate(time.Second)

	// Get key bindings based on current page
	var keyBindings []views.KeyBinding
	if m.currentPage == pageConversation {
		keyBindings = []views.KeyBinding{
			{Keys: "q", Description: "quit"},
			{Keys: "esc", Description: "back"},
			{Keys: "tab", Description: "focus turns/detail"},
			{Keys: "↑/↓", Description: "navigate"},
		}
	} else {
		keyBindings = []views.KeyBinding{
			{Keys: "q", Description: "quit"},
			{Keys: "tab", Description: "cycle focus"},
			{Keys: "enter", Description: "open conversation"},
			{Keys: "↑/↓", Description: "navigate/scroll"},
		}
	}

	return views.RenderWithChrome(
		views.ChromeConfig{
			Width:          m.width,
			Height:         m.height,
			ConfigFile:     m.configFile,
			CompletedCount: m.completedCount,
			TotalRuns:      m.totalRuns,
			Elapsed:        elapsed,
			KeyBindings:    keyBindings,
		},
		func(contentHeight int) string {
			switch m.currentPage {
			case pageConversation:
				return m.renderConversationPage(contentHeight)
			case pageMain:
				return m.renderMainPage(contentHeight)
			default:
				return m.renderMainPage(contentHeight)
			}
		},
	)
}

func (m *Model) renderMainPage(contentHeight int) string {
	// Convert model data to panel format
	runs := m.convertToRunInfos()
	logs := m.convertToLogEntries()

	// Determine focused panel
	focusedPanel := ""
	switch m.activePane {
	case paneRuns:
		focusedPanel = "runs"
	case paneLogs:
		focusedPanel = "logs"
	case paneResult:
		focusedPanel = "result"
	}

	// Get result data for highlighted run (cursor position)
	var resultData *panels.ResultPanelData
	if m.stateStore != nil && m.mainPage != nil {
		table := m.mainPage.RunsPanel().Table()
		idx := table.Cursor()
		if idx >= 0 && idx < len(m.activeRuns) {
			highlighted := &m.activeRuns[idx]
			ctx := m.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			if res, err := m.stateStore.GetResult(ctx, highlighted.RunID); err == nil {
				resultData = &panels.ResultPanelData{
					Result: res,
					Status: views.RunStatus(highlighted.Status),
				}
			}
		}
	}

	m.mainPage.SetDimensions(m.width, contentHeight)
	m.mainPage.SetData(runs, logs, focusedPanel, resultData)
	return m.mainPage.Render()
}

func (m *Model) renderConversationPage(contentHeight int) string {
	selected := m.selectedRun()
	if selected == nil {
		return "Select a run to view the conversation."
	}
	if m.stateStore == nil {
		return "No state store attached."
	}

	// Just set dimensions and render - data was initialized in toggleSelection
	m.conversationPage.SetDimensions(m.width, contentHeight)
	return m.conversationPage.Render()
}

// initializeConversationData loads initial conversation data when switching to conversation page
func (m *Model) initializeConversationData(run *RunInfo) {
	res := m.getConversationResult(run)
	m.conversationPage.SetData(run.RunID, run.Scenario, run.Provider, res)
	m.applySystemPromptFromCache(run.RunID)
}

// getConversationResult returns the conversation result for display.
// For running conversations, uses cached messages from real-time events.
// For completed runs, loads from state store with fallback to cache.
func (m *Model) getConversationResult(run *RunInfo) *statestore.RunResult {
	// For running conversations, use cached messages
	if run.Status == StatusRunning {
		return m.resultFromCache(run.RunID)
	}

	// For completed runs, try state store first
	if m.stateStore != nil {
		ctx := m.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		if res, err := m.stateStore.GetResult(ctx, run.RunID); err == nil {
			return res
		}
		logger.Debug("Could not load from state store, using cached messages", "run_id", run.RunID)
	}

	// Fall back to cached messages
	return m.resultFromCache(run.RunID)
}

// resultFromCache creates a RunResult from cached messages for a conversation.
func (m *Model) resultFromCache(runID string) *statestore.RunResult {
	messages := []types.Message{}
	if m.conversationMessages != nil {
		if cached, ok := m.conversationMessages[runID]; ok {
			messages = cached
		}
	}
	return &statestore.RunResult{Messages: messages}
}

// applySystemPromptFromCache prepends cached system prompt to the conversation panel if available.
func (m *Model) applySystemPromptFromCache(runID string) {
	if m.systemPrompts == nil {
		return
	}
	systemPrompt, ok := m.systemPrompts[runID]
	if !ok {
		return
	}
	panel := m.conversationPage.Panel()
	if panel == nil || panel.HasSystemPrompt() {
		return
	}
	newMsg := types.Message{
		Role:    "system",
		Content: systemPrompt,
	}
	panel.PrependSystemPrompt(&newMsg)
	logger.Debug("System prompt set from cache during initialization")
}

// convertToRunInfos converts model's activeRuns to panel RunInfo format
func (m *Model) convertToRunInfos() []panels.RunInfo {
	runs := make([]panels.RunInfo, len(m.activeRuns))
	for i := range m.activeRuns {
		runs[i] = panels.RunInfo{
			RunID:            m.activeRuns[i].RunID,
			Scenario:         m.activeRuns[i].Scenario,
			Provider:         m.activeRuns[i].Provider,
			Region:           m.activeRuns[i].Region,
			Status:           panels.RunStatus(m.activeRuns[i].Status),
			Duration:         m.activeRuns[i].Duration,
			Cost:             m.activeRuns[i].Cost,
			Error:            m.activeRuns[i].Error,
			StartTime:        m.activeRuns[i].StartTime,
			CurrentTurnIndex: m.activeRuns[i].CurrentTurnIndex,
			CurrentTurnRole:  m.activeRuns[i].CurrentTurnRole,
			Selected:         m.activeRuns[i].Selected,
		}
	}
	return runs
}

// convertToLogEntries converts model's logs to panel LogEntry format
func (m *Model) convertToLogEntries() []panels.LogEntry {
	logs := make([]panels.LogEntry, len(m.logs))
	for i := range m.logs {
		logs[i] = panels.LogEntry{
			Level:   m.logs[i].Level,
			Message: m.logs[i].Message,
		}
	}
	return logs
}


//nolint:unused // used by tests (golangci-lint runs with tests:false)
func (m *Model) currentRunForDetail() *RunInfo {
	if sel := m.selectedRun(); sel != nil {
		return sel
	}
	if m.mainPage != nil {
		table := m.mainPage.RunsPanel().Table()
		idx := table.Cursor()
		if idx >= 0 && idx < len(m.activeRuns) {
			return &m.activeRuns[idx]
		}
	}
	if len(m.activeRuns) > 0 {
		return &m.activeRuns[0]
	}
	return nil
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Check for quit keys first
	//nolint:exhaustive // Only handling specific quit keys
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyRunes:
		if len(msg.Runes) > 0 && msg.Runes[0] == 'q' {
			return m, tea.Quit
		}
	default:
	}

	// Escape deselects a conversation and returns to main view
	if msg.Type == tea.KeyEsc && m.currentPage == pageConversation {
		m.deselectRuns()
		m.conversationPage.Reset()
		m.currentPage = pageMain
		m.activePane = paneRuns
		return m, nil
	}

	if m.currentPage == pageMain {
		return m.handleMainPageKey(msg)
	}

	// Conversation page key handling
	cmd := m.conversationPage.Update(msg)
	return m, cmd
}

func (m *Model) handleMainPageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyTab {
		m.cycleFocusedPane()
		return m, nil
	}

	if msg.Type == tea.KeyEnter && m.activePane == paneRuns {
		m.toggleSelection()
		return m, nil
	}

	cmd := m.delegateKeyToActivePane(msg)
	return m, cmd
}

// cycleFocusedPane cycles through panes: runs -> logs -> result -> runs
func (m *Model) cycleFocusedPane() {
	switch m.activePane {
	case paneRuns:
		m.setFocusToLogsPane()
	case paneLogs:
		m.setFocusToResultPane()
	case paneResult:
		m.setFocusToRunsPane()
	}
}

// setFocusToRunsPane sets focus to the runs panel
func (m *Model) setFocusToRunsPane() {
	m.activePane = paneRuns
	m.mainPage.SetFocusedPanel("runs")
	if m.mainPage.ResultPanel() != nil {
		m.mainPage.ResultPanel().SetFocus(false)
	}
	m.mainPage.RunsPanel().SetFocus(true)
}

// setFocusToLogsPane sets focus to the logs panel
func (m *Model) setFocusToLogsPane() {
	m.activePane = paneLogs
	m.mainPage.SetFocusedPanel("logs")
	m.mainPage.RunsPanel().SetFocus(false)
	if m.mainPage.LogsPanel() != nil {
		m.mainPage.LogsPanel().SetFocus(true)
	}
}

// setFocusToResultPane sets focus to the result panel
func (m *Model) setFocusToResultPane() {
	m.activePane = paneResult
	m.mainPage.SetFocusedPanel("result")
	if m.mainPage.LogsPanel() != nil {
		m.mainPage.LogsPanel().SetFocus(false)
	}
	if m.mainPage.ResultPanel() != nil {
		m.mainPage.ResultPanel().SetFocus(true)
	}
}

// delegateKeyToActivePane forwards key events to the currently active panel
func (m *Model) delegateKeyToActivePane(msg tea.KeyMsg) tea.Cmd {
	switch m.activePane {
	case paneRuns:
		return m.handleRunsPaneKey(msg)
	case paneLogs:
		return m.handleLogsPaneKey(msg)
	case paneResult:
		return m.handleResultPaneKey(msg)
	default:
		return nil
	}
}

// handleRunsPaneKey handles key events for the runs panel
func (m *Model) handleRunsPaneKey(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	table := m.mainPage.RunsPanel().Table()
	*table, cmd = table.Update(msg)
	return cmd
}

// handleLogsPaneKey handles key events for the logs panel
func (m *Model) handleLogsPaneKey(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	viewport := m.mainPage.LogsPanel().Viewport()
	*viewport, cmd = viewport.Update(msg)
	return cmd
}

// handleResultPaneKey handles key events for the result panel
func (m *Model) handleResultPaneKey(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	if m.mainPage.ResultPanel() != nil {
		viewport := m.mainPage.ResultPanel().Viewport()
		if viewport != nil {
			*viewport, cmd = viewport.Update(msg)
		}
	}
	return cmd
}

func (m *Model) toggleSelection() {
	table := m.mainPage.RunsPanel().Table()
	idx := table.Cursor()
	if idx < 0 || idx >= len(m.activeRuns) {
		return
	}

	targetSelected := m.activeRuns[idx].Selected
	m.deselectRuns()
	m.activeRuns[idx].Selected = !targetSelected
	if m.activeRuns[idx].Selected {
		m.conversationPage.Reset()
		m.currentPage = pageConversation

		// Initialize conversation data when switching to conversation page
		m.initializeConversationData(&m.activeRuns[idx])
	}
}

func (m *Model) deselectRuns() {
	for i := range m.activeRuns {
		m.activeRuns[i].Selected = false
	}
}

func (m *Model) selectedRun() *RunInfo {
	for i := range m.activeRuns {
		if m.activeRuns[i].Selected {
			return &m.activeRuns[i]
		}
	}
	return nil
}

// BuildSummary creates a Summary from the current model state with output directory and HTML report path.
// Thread-safe for external callers.
func (m *Model) BuildSummary(outputDir, htmlReport string) *Summary {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buildSummary(outputDir, htmlReport)
}

// buildSummary is the internal implementation. Caller must hold m.mu.
func (m *Model) buildSummary(outputDir, htmlReport string) *Summary {
	// If a state store is available, try to reconstruct results for richer summary
	if m.stateStore != nil && len(m.activeRuns) > 0 {
		return m.buildSummaryFromStateStore(outputDir, htmlReport)
	}

	// Calculate average duration
	avgDuration := time.Duration(0)
	if m.completedCount > 0 {
		avgDuration = m.totalDuration / time.Duration(m.completedCount)
	}

	// Collect provider counts
	providerCounts := make(map[string]int)
	scenarioSet := make(map[string]bool)
	regionSet := make(map[string]bool)
	errors := make([]ErrorInfo, 0)

	for i := range m.activeRuns {
		run := &m.activeRuns[i]
		providerCounts[run.Provider]++
		scenarioSet[run.Scenario] = true
		regionSet[run.Region] = true

		if run.Status == StatusFailed {
			errors = append(errors, ErrorInfo{
				RunID:    run.RunID,
				Scenario: run.Scenario,
				Provider: run.Provider,
				Region:   run.Region,
				Error:    run.Error,
			})
		}
	}

	regions := make([]string, 0, len(regionSet))
	for region := range regionSet {
		regions = append(regions, region)
	}

	return &Summary{
		TotalRuns:      m.totalRuns,
		SuccessCount:   m.successCount,
		FailedCount:    m.failedCount,
		TotalCost:      m.totalCost,
		TotalTokens:    0, // Tokens only available via statestore
		TotalDuration:  time.Since(m.startTime),
		AvgDuration:    avgDuration,
		ProviderCounts: providerCounts,
		ScenarioCount:  len(scenarioSet),
		Regions:        regions,
		Errors:         errors,
		OutputDir:      outputDir,
		HTMLReport:     htmlReport,
	}
}

// buildSummaryFromStateStore builds summary using persisted run results (assertions, tool stats).
func (m *Model) buildSummaryFromStateStore(outputDir, htmlReport string) *Summary {
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	totalRuns := len(m.activeRuns)
	success := 0
	failed := 0
	var totalCost float64
	var totalTokens int64
	var totalDuration time.Duration
	providerCounts := make(map[string]int)
	scenarioSet := make(map[string]bool)
	regionSet := make(map[string]bool)
	errors := make([]ErrorInfo, 0)
	assertTotal := 0
	assertFailed := 0

	for i := range m.activeRuns {
		run := &m.activeRuns[i]
		res, err := m.stateStore.GetResult(ctx, run.RunID)
		if err != nil {
			errors = append(errors, ErrorInfo{
				RunID:    run.RunID,
				Scenario: run.Scenario,
				Provider: run.Provider,
				Region:   run.Region,
				Error:    err.Error(),
			})
			failed++
			continue
		}

		providerCounts[res.ProviderID]++
		scenarioSet[res.ScenarioID] = true
		regionSet[res.Region] = true

		totalCost += res.Cost.TotalCost
		totalTokens += int64(res.Cost.InputTokens + res.Cost.OutputTokens + res.Cost.CachedTokens)
		totalDuration += res.Duration

		if res.Error != "" {
			failed++
			errors = append(errors, ErrorInfo{
				RunID:    res.RunID,
				Scenario: res.ScenarioID,
				Provider: res.ProviderID,
				Region:   res.Region,
				Error:    res.Error,
			})
		} else {
			success++
		}

		assertTotal += res.ConversationAssertions.Total
		assertFailed += res.ConversationAssertions.Failed
	}

	regions := make([]string, 0, len(regionSet))
	for region := range regionSet {
		regions = append(regions, region)
	}

	avgDuration := time.Duration(0)
	if success+failed > 0 {
		avgDuration = totalDuration / time.Duration(success+failed)
	}

	return &Summary{
		TotalRuns:       totalRuns,
		SuccessCount:    success,
		FailedCount:     failed,
		TotalCost:       totalCost,
		TotalTokens:     totalTokens,
		TotalDuration:   totalDuration,
		AvgDuration:     avgDuration,
		ProviderCounts:  providerCounts,
		ScenarioCount:   len(scenarioSet),
		Regions:         regions,
		Errors:          errors,
		OutputDir:       outputDir,
		HTMLReport:      htmlReport,
		AssertionTotal:  assertTotal,
		AssertionFailed: assertFailed,
	}
}

// NewModel creates a new TUI model with the specified configuration file and total run count.
func NewModel(configFile string, totalRuns int) *Model {
	width, height, supported, reason := CheckTerminalSize()

	// Ensure we always have minimum dimensions
	if width < MinTerminalWidth {
		width = MinTerminalWidth
	}
	if height < MinTerminalHeight {
		height = MinTerminalHeight
	}

	return &Model{
		configFile:           configFile,
		totalRuns:            totalRuns,
		startTime:            time.Now(),
		activeRuns:           make([]RunInfo, 0),
		logs:                 make([]LogEntry, 0, maxLogBufferSize),
		width:                width,
		height:               height,
		isTUIMode:            supported,
		fallbackReason:       reason,
		ctx:                  context.Background(),
		mainPage:             pages.NewMainPage(),
		conversationPage:     pages.NewConversationPage(),
		currentPage:          pageMain,
		activePane:           paneRuns,
		systemPrompts:        make(map[string]string),
		conversationMessages: make(map[string][]types.Message),
	}
}

// SetStateStore attaches a state store for building summaries from run results.
func (m *Model) SetStateStore(store runResultStorer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateStore = store
}

// CompletedCount returns the number of completed runs (success + failure).
// Safe for concurrent use.
func (m *Model) CompletedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.completedCount
}

// ActiveRuns returns a snapshot of active runs for inspection or testing.
// Safe for concurrent use.
func (m *Model) ActiveRuns() []RunInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	cpy := make([]RunInfo, len(m.activeRuns))
	copy(cpy, m.activeRuns)
	return cpy
}

// Logs returns a snapshot of the current log entries.
// Safe for concurrent use.
func (m *Model) Logs() []LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	cpy := make([]LogEntry, len(m.logs))
	copy(cpy, m.logs)
	return cpy
}

// Run starts the TUI application
func Run(ctx context.Context, model *Model) error {
	if !model.isTUIMode {
		return fmt.Errorf("TUI mode not supported: %s", model.fallbackReason)
	}

	p := tea.NewProgram(model)

	errCh := make(chan error, 1)
	go func() {
		if _, err := p.Run(); err != nil {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		p.Quit()
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// Summary represents the final execution summary displayed after all runs complete
type Summary struct {
	TotalRuns      int
	SuccessCount   int
	FailedCount    int
	TotalCost      float64
	TotalTokens    int64
	TotalDuration  time.Duration
	AvgDuration    time.Duration
	ProviderCounts map[string]int
	ScenarioCount  int
	Regions        []string
	Errors         []ErrorInfo
	OutputDir      string
	HTMLReport     string

	AssertionTotal  int
	AssertionFailed int
}

// ErrorInfo represents a failed run with details
type ErrorInfo struct {
	RunID    string
	Scenario string
	Provider string
	Region   string
	Error    string
}

// CheckTerminalSize checks if the terminal is large enough for TUI mode
func CheckTerminalSize() (width, height int, supported bool, reason string) {
	// Try stdout first (fd 1), then stderr (fd 2), then stdin (fd 0)
	// This ensures TUI works even when stdin/stdout are redirected
	for _, fd := range []int{1, 2, 0} {
		width, height, err := term.GetSize(fd)
		if err == nil {
			if width < MinTerminalWidth || height < MinTerminalHeight {
				return width, height, false, fmt.Sprintf(
					"terminal too small (%dx%d, minimum %dx%d required)",
					width,
					height,
					MinTerminalWidth,
					MinTerminalHeight,
				)
			}
			return width, height, true, ""
		}
	}

	return 0, 0, false, "unable to detect terminal size (not a TTY)"
}

// RenderSummary renders the final summary screen for TUI mode
func RenderSummary(summary *Summary, width int) string {
	summaryData := convertSummaryToData(summary)
	summaryVM := viewmodels.NewSummaryViewModel(summaryData)
	summaryView := views.NewSummaryView(width, false)
	return summaryView.Render(summaryVM)
}

// RenderSummaryCIMode renders the summary in plain text for CI/non-TUI environments
func RenderSummaryCIMode(summary *Summary) string {
	const ciModeWidth = 80
	summaryData := convertSummaryToData(summary)
	summaryVM := viewmodels.NewSummaryViewModel(summaryData)
	summaryView := views.NewSummaryView(ciModeWidth, true)
	return summaryView.Render(summaryVM)
}

// convertSummaryToData converts old Summary struct to new SummaryData for viewmodels
func convertSummaryToData(summary *Summary) *viewmodels.SummaryData {
	// Convert provider counts to provider stats
	providerStats := make(map[string]viewmodels.ProviderStat)
	for provider, count := range summary.ProviderCounts {
		providerStats[provider] = viewmodels.ProviderStat{
			Runs:   count,
			Tokens: 0, // Tokens not available in old Summary
		}
	}

	// Convert errors to new ErrorInfo format
	errors := make([]viewmodels.ErrorInfo, len(summary.Errors))
	for i, errInfo := range summary.Errors {
		errors[i] = viewmodels.ErrorInfo{
			RunID:    errInfo.RunID,
			Scenario: errInfo.Scenario,
			Provider: errInfo.Provider,
			Region:   errInfo.Region,
			Error:    errInfo.Error,
		}
	}

	return &viewmodels.SummaryData{
		TotalRuns:       summary.TotalRuns,
		CompletedRuns:   summary.SuccessCount,
		FailedRuns:      summary.FailedCount,
		TotalTokens:     summary.TotalTokens,
		TotalCost:       summary.TotalCost,
		TotalDuration:   summary.TotalDuration,
		AvgDuration:     summary.AvgDuration,
		ProviderStats:   providerStats,
		ProviderCosts:   make(map[string]float64), // Not available in old Summary
		FailuresByError: make(map[string]int),     // Not available in old Summary
		ScenarioCount:   summary.ScenarioCount,
		Regions:         summary.Regions,
		Errors:          errors,
		OutputDir:       summary.OutputDir,
		HTMLReport:      summary.HTMLReport,
		AssertionTotal:  summary.AssertionTotal,
		AssertionFailed: summary.AssertionFailed,
	}
}
