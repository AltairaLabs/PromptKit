package pack

import (
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

// The PromptPack data types are runtime-owned — the runtime is the source of
// truth for the on-disk pack format (Arena and packc build and test packs
// through the runtime). This package aliases those types and adds only the
// SDK-side behavior (schema-validated loading, typed-error validation, and
// conversion into the runtime prompt.Registry / tool repository). There is no
// second definition of the pack format here.

// Pack is the runtime's PromptPack type.
type Pack = prompt.Pack

// Prompt is a single prompt definition within a pack.
type Prompt = prompt.PackPrompt

// Tool is a pack-level tool definition.
type Tool = prompt.PackTool

// Variable is a spec-exact compiled prompt template variable (no Binding).
type Variable = prompt.Variable

// Validator is a compiled, spec-exact pack validator.
type Validator = prompt.Validator

// ToolPolicy governs how a prompt may use tools.
type ToolPolicy = prompt.ToolPolicyPack

// MediaConfig is a prompt's multimodal media configuration.
type MediaConfig = prompt.MediaConfig

// Parameters holds model parameters for a prompt.
type Parameters = prompt.ParametersPack

// AgentsConfig maps prompts to A2A agent definitions.
type AgentsConfig = prompt.AgentsConfig

// AgentDef is A2A agent-card metadata for a single prompt.
type AgentDef = prompt.AgentDef

// SkillSourceConfig declares a skill source for the pack.
type SkillSourceConfig = prompt.SkillSourceConfig

// WorkflowSpec is a pack's workflow state-machine specification.
type WorkflowSpec = workflow.Spec

// WorkflowState is a single state within a workflow.
type WorkflowState = workflow.State

// WorkflowArtifactDef declares a workflow artifact.
type WorkflowArtifactDef = workflow.ArtifactDef
