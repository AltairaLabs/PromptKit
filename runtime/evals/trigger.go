package evals

import (
	"hash/fnv"
	"strconv"
)

// TriggerContext provides context for trigger evaluation decisions.
type TriggerContext struct {
	// SessionID identifies the current session (used for sampling).
	SessionID string

	// TurnIndex is the current turn number (used for sampling).
	TurnIndex int

	// IsSessionComplete indicates whether the session has ended.
	IsSessionComplete bool
}

// ShouldRun determines whether an eval should fire given its trigger,
// sampling percentage, and current context. Sampling is deterministic:
// the same sessionID+turnIndex always produces the same decision.
func ShouldRun(
	trigger EvalTrigger, samplePct float64, ctx *TriggerContext,
) bool {
	switch trigger {
	case TriggerEveryTurn, TriggerOnWorkflowStep:
		return true
	case TriggerOnSessionComplete, TriggerOnConversationComplete:
		return ctx.IsSessionComplete
	case TriggerSampleTurns:
		return sampleHit(ctx.SessionID, ctx.TurnIndex, samplePct)
	case TriggerSampleSessions:
		// Session sampling uses turnIndex=0 so every turn in the same
		// session gets the same decision.
		return sampleHit(ctx.SessionID, 0, samplePct)
	default:
		return false
	}
}

// sampleModulus is the modulus used for percentage-based sampling.
// pct (0–100) is multiplied by pctMultiplier so that integer comparison
// against hash % sampleModulus yields the correct hit rate.
const (
	sampleModulus = 10000
	pctMultiplier = 100
)

// sampleHit uses FNV-1a hashing for deterministic sampling.
// Returns true when hash(sessionID+turnIndex) % sampleModulus < pct * pctMultiplier.
func sampleHit(sessionID string, turnIndex int, pct float64) bool {
	h := fnv.New64a()
	_, _ = h.Write([]byte(sessionID))
	_, _ = h.Write([]byte(strconv.Itoa(turnIndex)))
	return h.Sum64()%sampleModulus < uint64(pct*pctMultiplier)
}
