package sdk

// LocalAgentExecutor routes A2A tool calls to in-process agents (single-prompt
// Conversations or workflow-backed members) instead of making remote HTTP
// calls. It implements tools.Executor.
type LocalAgentExecutor struct {
	members map[string]Agent
}

// NewLocalAgentExecutor creates an executor that routes tool calls to local agents.
func NewLocalAgentExecutor(members map[string]Agent) *LocalAgentExecutor {
	return &LocalAgentExecutor{members: members}
}

// Name returns the executor name. Must be "a2a" to intercept A2A tool dispatches.
func (e *LocalAgentExecutor) Name() string {
	return nsA2A
}
