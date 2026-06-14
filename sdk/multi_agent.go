package sdk

import (
	"context"
	"errors"
)

// Agent is a sendable agent pipeline. It is either a single-prompt
// *Conversation (RFC 0007) or a workflow-backed *WorkflowConversation entered
// at the agent's state (RFC 0011) — both are driven through Send identically.
type Agent interface {
	Send(ctx context.Context, message any, opts ...SendOption) (*Response, error)
	Close() error
}

// MultiAgentSession manages a set of agent members orchestrated through an
// entry agent. Tool calls from the entry agent to member agents are routed
// in-process via LocalAgentExecutor.
type MultiAgentSession struct {
	entry   Agent
	members map[string]Agent
}

// Entry returns the entry agent.
func (s *MultiAgentSession) Entry() Agent {
	return s.entry
}

// Members returns the member agents (excluding entry).
func (s *MultiAgentSession) Members() map[string]Agent {
	result := make(map[string]Agent, len(s.members))
	for k, v := range s.members {
		result[k] = v
	}
	return result
}

// Close closes all agents (entry and members).
// Errors from individual Close calls are collected and returned via errors.Join.
func (s *MultiAgentSession) Close() error {
	var errs []error
	if err := s.entry.Close(); err != nil {
		errs = append(errs, err)
	}
	for _, c := range s.members {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
