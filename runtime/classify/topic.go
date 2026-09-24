package classify

import "context"

// TopicDecision is a classifier's verdict about one user message.
type TopicDecision string

const (
	// TopicAllow means the message falls inside the declared scope.
	TopicAllow TopicDecision = "allow"
	// TopicDeny means the message falls outside it.
	TopicDeny TopicDecision = "deny"
	// TopicUnknown means the backend could not decide: an unreachable
	// service, or a response that did not parse. Callers map it to a
	// policy-configured outcome rather than guessing.
	TopicUnknown TopicDecision = "unknown"
)

// Small-talk dispositions. A policy says whether greetings, thanks and
// pleasantries — which match no listed subject — count as in scope.
const (
	SmallTalkAllow = "allow"
	SmallTalkDeny  = "deny"
)

// TopicExamples is an optional labeled corpus. It is a classification aid,
// not configuration: backends may fold it into a prompt or ignore it.
type TopicExamples struct {
	Allowed    []string
	Disallowed []string
}

// TopicPolicy is a declared conversational scope, in the pack author's own
// words. It is deliberately structured rather than a free-text criteria
// string: a chat-shaped backend renders it into a prompt, while a future
// embedding or ONNX backend can consume the same fields directly, and neither
// requires the pack to change.
type TopicPolicy struct {
	// Description is the application's purpose in prose. Required — an
	// allow-list alone reads as keywords to a semantic classifier.
	Description string
	// Allowed lists in-scope subjects. Required and non-empty: policy is
	// default-deny, so an empty list would block everything.
	Allowed []string
	// Disallowed lists explicit exclusions. Optional.
	Disallowed []string
	// SmallTalk is SmallTalkAllow or SmallTalkDeny.
	SmallTalk string
	// Examples is an optional labeled corpus.
	Examples TopicExamples
}

// TopicTurn is one prior conversation turn flattened to role and plain text.
//
// Deliberately not types.Message: assistant text lives on Content while user
// text can live in Parts, and the extraction helper belongs with the eval
// handlers. Flattening at the caller keeps this package free of a types
// dependency and stops every backend re-implementing the same extraction.
type TopicTurn struct {
	Role string // "user" or "assistant"
	Text string
}

// TopicRequest is one classification: judge Message against Policy, using
// History only to resolve references.
type TopicRequest struct {
	Policy  TopicPolicy
	History []TopicTurn
	// Message is the user message under judgment. Prior in-scope history
	// must not implicitly authorize it.
	Message string
}

// TopicResult is a classifier's answer.
type TopicResult struct {
	Decision TopicDecision
	// Confidence is populated only by a backend that genuinely has one.
	// A label-emitting model does not, and must leave it nil rather than
	// manufacture a number callers might threshold on.
	Confidence *float64
	// Reason is the backend's explanation where it supplies one.
	Reason string
	// Raw is the backend's unparsed answer, for diagnostics.
	Raw string
}

// TopicClassifier decides whether a user message falls inside a declared
// conversational scope. The sixth task interface in this package; see the
// package doc for how backends bind to tasks.
type TopicClassifier interface {
	ClassifyTopic(ctx context.Context, req TopicRequest) (TopicResult, error)
}
