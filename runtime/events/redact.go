package events

import "github.com/AltairaLabs/PromptKit/runtime/v2/types"

// Redaction of content-bearing event payloads, applied per SUBSCRIBER rather
// than at the source.
//
// Events carry customer data: tool arguments the model composed, tool return
// values, message content. Different consumers are entitled to different
// amounts of it — a trace exported to a third-party APM is not the same trust
// boundary as a local recording — so stripping content when the event is
// emitted would be wrong. It would take the payload away from consumers whose
// whole purpose is to hold it.
//
// Wrapping a subscriber instead lets each one get what it should:
//
//	bus.SubscribeAll(events.Redacting(traceListener.OnEvent, policy))
//	bus.SubscribeAll(auditStore.OnEvent) // unredacted, deliberately
//
// RecordingStage is unaffected either way: it appends straight to an EventStore
// without a bus hop, so lossless recording keeps full fidelity by construction.

// Field names passed to a Redactor, so a policy can treat them differently —
// scrub tool arguments but keep message content, say.
const (
	FieldMessageContent = "message.content"
	FieldToolCallArgs   = "tool_call.args"
	FieldToolResult     = "tool_result"
	FieldContentPart    = "content_part"

	// Eval payloads. An eval's output quotes what it judged — a judge's
	// reasoning restates the answer, a rubric cites the passage, a violation's
	// evidence IS the offending span — so all four carry customer text.
	FieldEvalValue       = "eval.value"
	FieldEvalExplanation = "eval.explanation"
	FieldEvalDetails     = "eval.details"
	FieldEvalEvidence    = "eval.violation_evidence"
)

// Redactor rewrites one content value. It receives the field name and the value
// that would otherwise be delivered, and returns the value to deliver. Return
// "" to blank the field.
//
// Called once per content value, so a policy can be as coarse as "replace
// everything" or as narrow as one regex on one field.
type Redactor func(field string, value string) string

// Redacting wraps a subscriber so it receives a redacted COPY of each event.
//
// The copy is the point. A bus fans one event out to every subscriber, so
// rewriting in place would redact for all of them — including the recording
// store that is supposed to keep the original. Each wrapped subscriber gets its
// own copy and the underlying event is never touched.
//
// A nil Redactor returns the subscriber unwrapped: no copy, no cost.
func Redacting(next func(*Event), r Redactor) func(*Event) {
	if r == nil || next == nil {
		return next
	}
	return func(e *Event) {
		next(redactEvent(e, r))
	}
}

// redactEvent returns a copy of e with content-bearing fields rewritten.
//
// Events whose payload carries no content are returned unchanged rather than
// copied — most events on a busy pipeline are provider and tool timing, and
// copying all of them to rewrite nothing would be pure overhead.
func redactEvent(e *Event, r Redactor) *Event {
	if e == nil {
		return nil
	}
	switch data := e.Data.(type) {
	case *MessageCreatedData:
		return withData(e, redactMessage(data, r))
	case *ToolCallEventData:
		return withData(e, redactToolCall(data, r))
	case *EvalEventData:
		return withData(e, redactEval(data, r))
	default:
		return e
	}
}

// withData shallow-copies the event envelope around new payload data. The
// envelope fields are identifiers and timestamps, never content.
func withData(e *Event, data EventData) *Event {
	cp := *e
	cp.Data = data
	return &cp
}

// redactMessage copies a message payload and rewrites its content.
func redactMessage(d *MessageCreatedData, r Redactor) *MessageCreatedData {
	cp := *d
	cp.Content = r(FieldMessageContent, d.Content)
	cp.Parts = redactParts(d.Parts, r)

	if len(d.ToolCalls) > 0 {
		calls := make([]MessageToolCall, len(d.ToolCalls))
		copy(calls, d.ToolCalls)
		for i := range calls {
			calls[i].Args = r(FieldToolCallArgs, calls[i].Args)
		}
		cp.ToolCalls = calls
	}
	if d.ToolResult != nil {
		// A tool result carries its payload in Parts, not a Content string —
		// the result is multimodal. Error text is a message about the failure,
		// not tool output, but it routinely quotes the arguments that caused
		// it, so it is redacted too.
		res := *d.ToolResult
		res.Parts = redactParts(res.Parts, r)
		if res.Error != "" {
			res.Error = r(FieldToolResult, res.Error)
		}
		cp.ToolResult = &res
	}
	return &cp
}

// redactToolCall copies a tool-call payload and rewrites its arguments and
// result parts.
//
// Args is map[string]any: values are rewritten only where they are strings,
// since a Redactor rewrites text. A non-string value that carries a secret is
// out of reach here and needs the field dropped by the policy that owns it.
func redactToolCall(d *ToolCallEventData, r Redactor) *ToolCallEventData {
	cp := *d
	if len(d.Args) > 0 {
		args := make(map[string]interface{}, len(d.Args))
		for k, v := range d.Args {
			if s, ok := v.(string); ok {
				args[k] = r(FieldToolCallArgs, s)
				continue
			}
			args[k] = v
		}
		cp.Args = args
	}
	cp.Parts = redactParts(d.Parts, r)
	return &cp
}

// redactParts copies content parts and rewrites their text.
func redactParts(parts []types.ContentPart, r Redactor) []types.ContentPart {
	if len(parts) == 0 {
		return parts
	}
	out := make([]types.ContentPart, len(parts))
	copy(out, parts)
	for i := range out {
		if out[i].Text == nil {
			continue
		}
		scrubbed := r(FieldContentPart, *out[i].Text)
		out[i].Text = &scrubbed
	}
	return out
}

// redactEval copies an eval payload and rewrites the fields that quote what was
// judged.
//
// Score, MetricValue, Passed, Kind and the timings are measurements ABOUT
// content, not content, and are left alone — redacting them would leave a
// consumer unable to tell a blocked guardrail from a passing one, which is the
// thing an audit most needs. Message is the pack author's own configured text,
// not customer data, and is left alone for the same reason.
func redactEval(d *EvalEventData, r Redactor) *EvalEventData {
	cp := *d
	if d.Explanation != "" {
		cp.Explanation = r(FieldEvalExplanation, d.Explanation)
	}
	if d.Value != nil {
		cp.Value = redactAny(FieldEvalValue, d.Value, r)
	}
	if len(d.Details) > 0 {
		details := make(map[string]any, len(d.Details))
		for k, v := range d.Details {
			details[k] = redactAny(FieldEvalDetails, v, r)
		}
		cp.Details = details
	}
	if len(d.Violations) > 0 {
		cp.Violations = redactViolations(d.Violations, r)
	}
	return &cp
}

// redactViolations copies violations and rewrites the text in each.
//
// Evidence is the quoted span that tripped the eval — the single most
// content-bearing field on an eval event, and the one a policy is most likely
// to be scrubbing for.
func redactViolations(in []EvalViolationData, r Redactor) []EvalViolationData {
	out := make([]EvalViolationData, len(in))
	copy(out, in)
	for i := range out {
		if out[i].Description != "" {
			out[i].Description = r(FieldEvalExplanation, out[i].Description)
		}
		if len(out[i].Evidence) == 0 {
			continue
		}
		evidence := make(map[string]any, len(out[i].Evidence))
		for k, v := range out[i].Evidence {
			evidence[k] = redactAny(FieldEvalEvidence, v, r)
		}
		out[i].Evidence = evidence
	}
	return out
}

// redactAny rewrites the strings inside an arbitrary handler value.
//
// An eval's value is whatever its handler returned — a bare string, a rubric's
// map of criterion to comment, a list of quoted spans — so a redactor that only
// handled top-level strings would pass the nested ones straight through, which
// is where the quoted text actually lives.
//
// Containers are COPIED, never rewritten in place: the same value is delivered
// to every other subscriber, including the ones entitled to see it unredacted.
// Numbers and bools are returned as-is; they carry no text to rewrite.
func redactAny(field string, v any, r Redactor) any {
	switch val := v.(type) {
	case string:
		return r(field, val)
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, inner := range val {
			out[k] = redactAny(field, inner, r)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, inner := range val {
			out[i] = redactAny(field, inner, r)
		}
		return out
	case []string:
		out := make([]string, len(val))
		for i, inner := range val {
			out[i] = r(field, inner)
		}
		return out
	default:
		return v
	}
}
