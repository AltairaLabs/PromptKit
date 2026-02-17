package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/stretchr/testify/assert"
)

// stubCapability is a test capability.
type stubCapability struct {
	name      string
	initErr   error
	initCalls int
	closeCalls int
}

func (s *stubCapability) Name() string                     { return s.name }
func (s *stubCapability) Init(_ CapabilityContext) error    { s.initCalls++; return s.initErr }
func (s *stubCapability) RegisterTools(_ *tools.Registry)  {}
func (s *stubCapability) Close() error                     { s.closeCalls++; return nil }

func TestInferCapabilities_WithWorkflow(t *testing.T) {
	p := &pack.Pack{
		Workflow: &pack.WorkflowSpec{
			Version: 1,
			Entry:   "start",
			States:  map[string]*pack.WorkflowState{},
		},
	}
	caps := inferCapabilities(p)
	assert.Len(t, caps, 1)
	assert.Equal(t, "workflow", caps[0].Name())
}

func TestInferCapabilities_NoWorkflow(t *testing.T) {
	p := &pack.Pack{}
	caps := inferCapabilities(p)
	assert.Empty(t, caps)
}

func TestInferCapabilities_WithAgents(t *testing.T) {
	p := &pack.Pack{
		Agents: &pack.AgentsConfig{
			Entry: "orchestrator",
			Members: map[string]*pack.AgentDef{
				"helper": {Description: "A helper"},
			},
		},
	}
	caps := inferCapabilities(p)
	assert.Len(t, caps, 1)
	assert.Equal(t, "a2a", caps[0].Name())
}

func TestInferCapabilities_WithWorkflowAndAgents(t *testing.T) {
	p := &pack.Pack{
		Workflow: &pack.WorkflowSpec{
			Version: 1,
			Entry:   "start",
		},
		Agents: &pack.AgentsConfig{
			Entry: "orchestrator",
			Members: map[string]*pack.AgentDef{
				"helper": {Description: "A helper"},
			},
		},
	}
	caps := inferCapabilities(p)
	assert.Len(t, caps, 2)
	names := []string{caps[0].Name(), caps[1].Name()}
	assert.Contains(t, names, "workflow")
	assert.Contains(t, names, "a2a")
}

func TestMergeCapabilities_ExplicitOverridesInferred(t *testing.T) {
	explicit := []Capability{&stubCapability{name: "workflow"}}
	inferred := []Capability{&stubCapability{name: "workflow"}, &stubCapability{name: "a2a"}}

	merged := mergeCapabilities(explicit, inferred)
	assert.Len(t, merged, 2)
	// The explicit workflow should be first, then inferred a2a
	assert.Equal(t, "workflow", merged[0].Name())
	assert.Equal(t, "a2a", merged[1].Name())
	// The explicit one should be the same pointer
	assert.Same(t, explicit[0], merged[0])
}

func TestMergeCapabilities_NoDuplicates(t *testing.T) {
	cap1 := &stubCapability{name: "workflow"}
	cap2 := &stubCapability{name: "workflow"}

	merged := mergeCapabilities([]Capability{cap1}, []Capability{cap2})
	assert.Len(t, merged, 1)
	assert.Same(t, cap1, merged[0])
}

func TestMergeCapabilities_BothEmpty(t *testing.T) {
	merged := mergeCapabilities(nil, nil)
	assert.Empty(t, merged)
}

func TestMergeCapabilities_OnlyInferred(t *testing.T) {
	inferred := []Capability{&stubCapability{name: "a2a"}}
	merged := mergeCapabilities(nil, inferred)
	assert.Len(t, merged, 1)
	assert.Equal(t, "a2a", merged[0].Name())
}
