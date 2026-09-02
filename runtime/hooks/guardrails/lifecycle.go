package guardrails

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// funcValidatorType is the reported validator type for guardrails declared from
// a plain function. They have no eval type, but the OTel listener maps this to
// promptkit.eval.type, so reporting "func" is more useful than reporting the
// caller's chosen name twice.
const funcValidatorType = "func"

// lifecycle emits the validation events for one guardrail evaluation.
//
// Both guardrail kinds — eval-backed and func-backed — need the same pair, and
// they need it emitted the same way: started opens the OTel span (the only
// place promptkit.guardrail=true is set), passed closes it and supplies the
// denominator the firing metrics lack. Writing that twice is how the input and
// output directions drifted before, so it lives here once.
//
// Firings are deliberately absent: ProviderStage.recordGuardrailFiring emits
// validation.failed for every guardrail, and a second emission would
// double-count every one.
type lifecycle struct {
	emitter   *events.Emitter
	name      string
	valType   string
	direction string
	turnIndex int
}

// start emits validation.started and returns the instant to measure from.
// Safe with no emitter, in which case nothing is published.
//
// GuardrailStarted rather than ValidationStarted: the two-string form cannot
// carry TurnIndex, and a started event that cannot be placed against a turn is
// the half of the pair that leaves a firing rate uncomputable.
func (l lifecycle) start() time.Time {
	if l.emitter != nil {
		l.emitter.GuardrailStarted(&events.ValidationEventData{
			ValidatorName: l.name,
			ValidatorType: l.valType,
			Direction:     l.direction,
			TurnIndex:     l.turnIndex,
		})
	}
	return time.Now()
}

// pass emits validation.passed for an evaluation that did not trigger. score is
// nil for guardrails that produce no score, such as func-backed ones.
func (l lifecycle) pass(since time.Time, score *float64) {
	if l.emitter == nil {
		return
	}
	data := &events.ValidationEventData{
		ValidatorName: l.name,
		ValidatorType: l.valType,
		Direction:     l.direction,
		Duration:      time.Since(since),
		TurnIndex:     l.turnIndex,
	}
	if score != nil {
		data.Score = *score
	}
	l.emitter.GuardrailResult(data)
}
