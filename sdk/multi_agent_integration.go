package sdk

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// OpenMultiAgent loads a multi-agent pack and creates conversations for all
// members and the entry agent. Agent-to-agent tool calls from the entry
// conversation are routed in-process via LocalAgentExecutor.
//
// The pack must have an agents section with entry and members defined.
// Options are applied to all conversations (entry and members).
func OpenMultiAgent(packPath string, opts ...Option) (*MultiAgentSession, error) {
	p, err := loadMultiAgentPack(packPath, opts)
	if err != nil {
		return nil, err
	}

	members, err := openMembers(packPath, p, opts)
	if err != nil {
		return nil, err
	}

	entry, err := openEntryAgent(packPath, p.Agents.Entry, members, opts)
	if err != nil {
		closeAll(members)
		return nil, fmt.Errorf("failed to open entry agent %q: %w", p.Agents.Entry, err)
	}

	return &MultiAgentSession{entry: entry, members: members}, nil
}

// Send sends a message through the entry agent.
func (s *MultiAgentSession) Send(
	ctx context.Context,
	message any,
	opts ...SendOption,
) (*Response, error) {
	return s.entry.Send(ctx, message, opts...)
}

// loadMultiAgentPack resolves, loads, and validates a multi-agent pack.
func loadMultiAgentPack(packPath string, opts []Option) (*pack.Pack, error) {
	absPath, err := resolvePackPath(packPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve pack path: %w", err)
	}

	baseCfg, err := applyOptions("", opts)
	if err != nil {
		return nil, err
	}

	p, err := pack.Load(absPath, pack.LoadOptions{
		SkipSchemaValidation: baseCfg.skipSchemaValidation,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load pack: %w", err)
	}

	if p.Agents == nil || len(p.Agents.Members) == 0 {
		return nil, fmt.Errorf("pack has no agents section")
	}
	if p.Agents.Entry == "" {
		return nil, fmt.Errorf("pack agents section has no entry defined")
	}

	return p, nil
}

// openMembers opens conversations for all non-entry agent members.
func openMembers(
	packPath string,
	p *pack.Pack,
	opts []Option,
) (map[string]*Conversation, error) {
	members := make(map[string]*Conversation)
	for name := range p.Agents.Members {
		if name == p.Agents.Entry {
			continue
		}
		conv, err := Open(packPath, name, opts...)
		if err != nil {
			closeAll(members)
			return nil, fmt.Errorf("failed to open member agent %q: %w", name, err)
		}
		members[name] = conv
	}
	return members, nil
}

// openEntryAgent opens the entry conversation with a local agent executor.
func openEntryAgent(
	packPath, entryName string,
	members map[string]*Conversation,
	opts []Option,
) (*Conversation, error) {
	localExec := NewLocalAgentExecutor(members)
	entryOpts := make([]Option, len(opts))
	copy(entryOpts, opts)
	entryOpts = append(entryOpts, withLocalAgentExecutor(localExec))
	return Open(packPath, entryName, entryOpts...)
}

// closeAll closes all conversations in the map, ignoring errors.
func closeAll(convs map[string]*Conversation) {
	for _, c := range convs {
		_ = c.Close()
	}
}
