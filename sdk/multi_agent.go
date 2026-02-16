package sdk

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
func (s *MultiAgentSession) Close() error {
	_ = s.entry.Close()
	for _, c := range s.members {
		_ = c.Close()
	}
	return nil
}
