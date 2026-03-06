package sdk

import "errors"

// MultiAgentSession manages a set of agent member conversations orchestrated
// through an entry conversation. Tool calls from the entry agent to member
// agents are routed in-process via LocalAgentExecutor.
type MultiAgentSession struct {
	entry   *Conversation
	members map[string]*Conversation
}

// Entry returns the entry conversation.
func (s *MultiAgentSession) Entry() *Conversation {
	return s.entry
}

// Members returns the member conversations (excluding entry).
func (s *MultiAgentSession) Members() map[string]*Conversation {
	result := make(map[string]*Conversation, len(s.members))
	for k, v := range s.members {
		result[k] = v
	}
	return result
}

// Close closes all conversations (entry and members).
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
