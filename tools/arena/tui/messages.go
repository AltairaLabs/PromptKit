package tui

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// RunStartedMsg is sent when a run begins execution.
type RunStartedMsg struct {
	RunID    string
	Scenario string
	Provider string
	Region   string
	Time     time.Time
}

// RunCompletedMsg is sent when a run completes successfully.
type RunCompletedMsg struct {
	RunID    string
	Duration time.Duration
	Cost     float64
	Time     time.Time
}

// RunFailedMsg is sent when a run fails with an error.
type RunFailedMsg struct {
	RunID string
	Error error
	Time  time.Time
}

// TurnStartedMsg is sent when a turn starts.
type TurnStartedMsg struct {
	RunID     string
	TurnIndex int
	Role      string
	Scenario  string
	Time      time.Time
}

// TurnCompletedMsg is sent when a turn completes.
type TurnCompletedMsg struct {
	RunID     string
	TurnIndex int
	Role      string
	Scenario  string
	Error     error
	Time      time.Time
}

// MessageToolCall is a type alias for events.MessageToolCall to avoid duplicate definitions.
type MessageToolCall = events.MessageToolCall

// MessageToolResult is a type alias for events.MessageToolResult to avoid duplicate definitions.
type MessageToolResult = events.MessageToolResult

// MessageCreatedMsg is sent when a message is created during execution.
type MessageCreatedMsg struct {
	ConversationID string
	Role           string
	Content        string
	Index          int
	ToolCalls      []MessageToolCall  // Tool calls requested by assistant
	ToolResult     *MessageToolResult // Tool result for tool messages
	Time           time.Time
}

// MessageUpdatedMsg is sent when a message is updated with cost/latency info.
type MessageUpdatedMsg struct {
	ConversationID string
	Index          int
	LatencyMs      int64
	InputTokens    int
	OutputTokens   int
	TotalCost      float64
	Time           time.Time
}

// ConversationStartedMsg is sent when a new conversation starts with its system prompt.
type ConversationStartedMsg struct {
	ConversationID string
	SystemPrompt   string
	Time           time.Time
}
