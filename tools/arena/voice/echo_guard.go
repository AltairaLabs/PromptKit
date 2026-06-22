package voice

import "sync/atomic"

// EchoGuard implements an optional half-duplex mic gate: while the agent is
// speaking, mic frames quieter than threshold are dropped (they are most likely
// the agent's own audio bleeding into the mic). Louder frames still pass so a
// deliberate barge-in interrupts. Off (always-allow) unless enabled by Run.
type EchoGuard struct {
	threshold     float32
	agentSpeaking atomic.Bool
}

// NewEchoGuard builds a guard with the given RMS threshold (0..1).
func NewEchoGuard(threshold float32) *EchoGuard {
	return &EchoGuard{threshold: threshold}
}

// SetAgentSpeaking marks whether agent audio is currently playing.
func (g *EchoGuard) SetAgentSpeaking(v bool) { g.agentSpeaking.Store(v) }

// Allow reports whether a mic frame should be forwarded.
func (g *EchoGuard) Allow(frame []byte) bool {
	if !g.agentSpeaking.Load() {
		return true
	}
	return rms(frame) >= g.threshold
}
