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

// Redactor rewrites a content-bearing span attribute before it is recorded.
//
// Called once per attribute, with the attribute key (e.g.
// "gen_ai.tool.call.arguments") and the value that would otherwise be recorded.
// Return the value to record; return "" to omit the attribute entirely.
//
// Only invoked when content capture is ENABLED — it is a scrubber for
// deliberately-captured content, not a substitute for the gate. A nil Redactor
// records values unchanged, which is what enabling capture without one asks for.
type Redactor func(attribute string, value string) string

// contentCapture holds the resolved policy for one listener.
type contentCapture struct {
	enabled  bool
	redactor Redactor
}

// value applies the policy to one content-bearing attribute.
//
// Returns the value to record and whether to record it at all. Everything
// content-bearing goes through here so a new attribute cannot be added on a
// path that skips the gate.
func (c contentCapture) value(attribute, raw string) (string, bool) {
	if !c.enabled {
		return "", false
	}
	if c.redactor == nil {
		return raw, true
	}
	scrubbed := c.redactor(attribute, raw)
	if scrubbed == "" {
		return "", false
	}
	return scrubbed, true
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
// data. Pair it with WithRedactor where tool arguments may carry credentials:
// on-behalf-of token exchange puts per-user delegated OAuth tokens into tool
// arguments, and those would otherwise land in the trace verbatim.
func WithContentCapture(enabled bool) OTelOption {
	return func(l *OTelEventListener) { l.content.enabled = enabled }
}

// WithRedactor sets the scrubber applied to content attributes when capture is
// enabled. It has no effect while capture is off, because nothing is recorded
// to scrub.
func WithRedactor(r Redactor) OTelOption {
	return func(l *OTelEventListener) { l.content.redactor = r }
}

// Content-bearing attribute keys, named so every producer routes through
// contentCapture.value rather than spelling a key inline and skipping the gate.
const (
	attrToolCallArguments = "gen_ai.tool.call.arguments"
	attrToolCalls         = "gen_ai.tool_calls"
	attrToolResult        = "gen_ai.tool_result"
	attrMessageContent    = "gen_ai.message.content"
)
