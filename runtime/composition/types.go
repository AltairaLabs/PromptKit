// Package composition defines RFC 0010 workflow-composition types and their
// validation. It is a leaf package: it imports neither runtime/pipeline nor
// runtime/workflow, so workflow.State can reference these types without a cycle.
package composition

// StepKind identifies a composition step's kind.
type StepKind string

// Step kinds (RFC 0010 v1).
const (
	KindPrompt   StepKind = "prompt"
	KindAgent    StepKind = "agent"
	KindTool     StepKind = "tool"
	KindBranch   StepKind = "branch"
	KindParallel StepKind = "parallel"
)

// Reduce strategies (RFC 0010 v1).
const (
	ReduceAppend  = "append"
	ReduceReplace = "replace"
	ReduceBarrier = "barrier"
)

// Composition is a named step graph over the pack's prompts, tools, and evals.
type Composition struct {
	Version      int            `json:"version"`
	Description  string         `json:"description,omitempty"`
	InputSchema  string         `json:"input_schema,omitempty"`
	OutputSchema string         `json:"output_schema,omitempty"`
	Output       string         `json:"output,omitempty"` // step id; default = last step
	Steps        []*Step        `json:"steps"`
	Engine       map[string]any `json:"engine,omitempty"` // opaque runtime-specific config
}

// Step is a single node in a composition graph. Only the fields legal for Kind
// are set; per-kind legality is enforced by Validate.
type Step struct {
	ID        string         `json:"id"`
	Kind      StepKind       `json:"kind"`
	DependsOn []string       `json:"depends_on,omitempty"`
	Modifiers *StepModifiers `json:"modifiers,omitempty"`

	// prompt, agent
	PromptTask   string       `json:"prompt_task,omitempty"`
	Input        any          `json:"input,omitempty"` // StepInput: "${...}" string or object
	OutputSchema string       `json:"output_schema,omitempty"`
	Tools        []string     `json:"tools,omitempty"`       // agent only
	Termination  *Termination `json:"termination,omitempty"` // agent only

	// tool
	Tool string         `json:"tool,omitempty"`
	Args map[string]any `json:"args,omitempty"`

	// branch
	Predicate *Predicate `json:"predicate,omitempty"`
	Then      string     `json:"then,omitempty"`
	Else      string     `json:"else,omitempty"`

	// parallel
	Branches []*Step  `json:"branches,omitempty"`
	Reduce   *Reducer `json:"reduce,omitempty"`
}

// Termination bounds an agent step's tool loop. anyOf max_steps | tool_called.
type Termination struct {
	MaxSteps   int    `json:"max_steps,omitempty"`
	ToolCalled string `json:"tool_called,omitempty"`
}

// Reducer declares how parallel branch outputs merge. Both fields required.
type Reducer struct {
	Strategy string `json:"strategy"` // append | replace | barrier
	Into     string `json:"into"`     // variable name the merged result is exposed under
}

// StepModifiers are optional per-step behaviors (RFC 0010 v1: retry, eval).
type StepModifiers struct {
	Retry *RetryModifier `json:"retry,omitempty"`
	Eval  []string       `json:"eval,omitempty"` // references to pack eval keys (observability)
}

// RetryModifier re-runs a step on error up to MaxAttempts.
type RetryModifier struct {
	MaxAttempts int `json:"max_attempts,omitempty"`
}

// Predicate is the constrained predicate language. Exactly one variant is set:
// compare (path+op+value), exists (path+exists), or a composite (all_of/any_of/not).
type Predicate struct {
	Path   string       `json:"path,omitempty"`
	Op     string       `json:"op,omitempty"`
	Value  any          `json:"value,omitempty"`
	Exists *bool        `json:"exists,omitempty"`
	AllOf  []*Predicate `json:"all_of,omitempty"`
	AnyOf  []*Predicate `json:"any_of,omitempty"`
	Not    *Predicate   `json:"not,omitempty"`
}

// compareOps is the set of valid comparison operators.
var compareOps = map[string]bool{
	"equals": true, "not_equals": true,
	"in": true, "not_in": true,
	"less_than": true, "less_than_or_equals": true,
	"greater_than": true, "greater_than_or_equals": true,
}
