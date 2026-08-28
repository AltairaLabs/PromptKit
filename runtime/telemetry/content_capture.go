package telemetry

// Content capture: whether conversation content and tool payloads are attached
// to spans, and how they are scrubbed when they are.
//
// These attributes carry customer data — tool arguments the model composed,
// tool return values, raw message content. Exporting a trace exports them with
// it, across whatever trust boundary the operator's APM sits behind. The OTel
// GenAI conventions make message-content capture opt-in for exactly this
// reason; this package used to emit it unconditionally with no way to turn it
// off short of disabling tracing.
//
// Why this cannot be left to the SDK caller, which is the obvious first
// suggestion: three of the four content attributes are produced by the MODEL,
// not by user code — tool-call arguments, the tool-call list, message content.
// The fourth, the tool result, does come from a user's handler, but redacting
// it there changes what the MODEL receives, since the pipeline and the trace
// read the same value. There is no caller-side lever that separates "what the
// trace records" from "what the pipeline uses", so the gate has to live here.
// What the caller CAN own is the policy, which is what Redactor is for.

// Redaction is NOT here. It lives in events.Redacting, which wraps any bus
// subscriber and hands it a redacted copy — so one policy covers this listener,
// a caller's own subscriber and any third-party event store, rather than each
// consumer growing its own hook:
//
//	bus.SubscribeAll(events.Redacting(listener.OnEvent, policy))
//
// What stays here is the GATE, because only a library-side default can stop
// content reaching a trace for someone who simply switched tracing on.

// contentCapture holds the resolved policy for one listener.
type contentCapture struct {
	enabled bool
}

// value applies the policy to one content-bearing attribute.
//
// Returns the value to record and whether to record it at all. Everything
// content-bearing goes through here so a new attribute cannot be added on a
// path that skips the gate.
func (c contentCapture) value(_, raw string) (string, bool) {
	if !c.enabled {
		return "", false
	}
	return raw, true
}

// OTelOption configures an OTelEventListener.
type OTelOption func(*OTelEventListener)

// WithContentCapture enables attaching conversation content and tool payloads
// to spans.
//
// OFF by default. Span structure, timing, token usage, model names and tool
// NAMES are unaffected — those carry the operational value without the payload,
// and remain on regardless.
//
// Enable this only where the trace backend is an appropriate place for customer
// data. Where tool arguments may carry credentials — on-behalf-of token
// exchange puts per-user delegated OAuth tokens there — wrap the subscriber in
// events.Redacting rather than reaching for a hook on this listener.
func WithContentCapture(enabled bool) OTelOption {
	return func(l *OTelEventListener) { l.content.enabled = enabled }
}

// Content-bearing attribute keys, named so every producer routes through
// contentCapture.value rather than spelling a key inline and skipping the gate.
const (
	attrToolCallArguments = "gen_ai.tool.call.arguments"
	attrToolCalls         = "gen_ai.tool_calls"
	attrToolResult        = "gen_ai.tool_result"
	attrMessageContent    = "gen_ai.message.content"
)
