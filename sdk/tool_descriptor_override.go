package sdk

import (
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// Errors returned by WithToolDescriptorOverride for misuse.
var (
	errEmptyToolOverrideName = errors.New("WithToolDescriptorOverride: tool name must not be empty")
	errNilToolOverrideFn     = errors.New("WithToolDescriptorOverride: patch function must not be nil")
)

// ToolDescriptorPatchFn mutates a tool descriptor in place.
//
// Patch functions receive a clone of the descriptor that the capability
// originally registered, so mutations cannot leak into other conversations
// or other consumers of the same registry.
//
// To replace the entire descriptor (rare), assign field-by-field. To wrap
// or augment the description, mutate d.Description. To extend the input
// schema, replace d.InputSchema with a new json.RawMessage.
type ToolDescriptorPatchFn func(d *tools.ToolDescriptor)

// toolDescriptorOverride pairs a tool name with a patch function.
type toolDescriptorOverride struct {
	name string
	fn   ToolDescriptorPatchFn
}

// WithToolDescriptorOverride patches a registered capability tool descriptor
// after capabilities have registered their defaults.
//
// This is a general-purpose hook: it works for memory tools, workflow tools,
// A2A tools, skills, and any future capability that registers tools through
// the standard Capability.RegisterTools path. Use it to customize tool
// descriptions, extend input schemas, or relabel namespaces without forking
// PromptKit.
//
// Multiple overrides for the same tool compose in registration order — the
// patch from the second WithToolDescriptorOverride sees the descriptor
// already mutated by the first.
//
// If no tool with the given name is registered when overrides are applied,
// the override is logged and skipped. This makes the option tolerant of
// version skew (a tool removed upstream does not break the consumer's
// override list).
//
// # Schema extension and host passthrough
//
// Extending the InputSchema with new top-level fields produces structured
// LLM args, but the executor's typed decode would historically drop unknown
// keys. Executors that have a host-facing callback now route unknown
// top-level args into that callback's typed record:
//
//   - memory__remember           → Memory.Metadata          (host's MemoryStore.Save)
//   - memory__recall             → RetrieveOptions.Extras   (host's MemoryStore.Retrieve)
//   - memory__list               → ListOptions.Extras       (host's MemoryStore.List)
//   - memory__forget             → DeleteOptions.Extras     (stores implementing memory.ExtrasDeleter)
//   - A2A outgoing tools         → Message.Metadata         (wire payload to remote agent)
//   - workflow__transition       → TransitionResult.HostExtras (host's OnCommit callback)
//
// For memory__remember and a2a the extras merge into the typed Metadata
// field with typed-fields-win on conflict. memory__recall and memory__list
// put them on a dedicated Extras field of the options struct they hand the
// store, so a store that does not recognize a key simply ignores it.
// Workflow uses a separate HostExtras field, which is reserved exclusively
// for this passthrough channel — the runtime never writes to it from
// elsewhere.
//
// memory__forget is the one that needs opting into: memory.Store.Delete has
// no options parameter, so a store receives the extras only if it implements
// memory.ExtrasDeleter. Stores that do not are unaffected and keep the plain
// Delete.
//
// Tools without a host-facing callback (workflow__set_artifact, the skills
// tools) currently drop unknown top-level fields. Extending those schemas
// will pass the new field to the LLM but the data is not observable
// host-side. If you need this for one of them, file an issue describing
// the use case so the right observation point can be designed.
//
// Example: customize the memory__remember tool's description for an Omnia
// deployment that wants the LLM to tag a category alongside the memory,
// and extend the schema with an `about` field that flows into Memory.Metadata.
//
//	conv, err := sdk.Open(packPath, "chat",
//	    sdk.WithMemory(store, scope),
//	    sdk.WithToolDescriptorOverride("memory__remember",
//	        func(d *tools.ToolDescriptor) {
//	            d.Description = "Store something in memory ..."
//	            d.InputSchema = customSchemaJSON // adds top-level `about`
//	        }),
//	)
//	// LLM calls memory__remember(content="...", about={kind:"preference",key:"seat"})
//	// → Memory.Metadata["about"] = {kind:..., key:...}
//
// Workflow transitions follow the same shape but use the dedicated
// HostExtras field on the workflow.transitioned event. Subscribe via the
// event bus to read extras after a transition commits:
//
//	conv, err := sdk.OpenWorkflow(packPath, "chat",
//	    sdk.WithEventBus(bus),
//	    sdk.WithToolDescriptorOverride("workflow__transition",
//	        func(d *tools.ToolDescriptor) {
//	            d.InputSchema = schemaWithEscalationReason
//	        }),
//	)
//	bus.Subscribe(events.EventWorkflowTransitioned, func(e events.Event) {
//	    d := e.Data.(*events.WorkflowTransitionedData)
//	    if reason, ok := d.HostExtras["escalation_reason"]; ok {
//	        audit.Record(d.Event, reason)
//	    }
//	})
func WithToolDescriptorOverride(name string, fn ToolDescriptorPatchFn) Option {
	return func(c *config) error {
		if name == "" {
			return errEmptyToolOverrideName
		}
		if fn == nil {
			return errNilToolOverrideFn
		}
		c.toolDescriptorOverrides = append(c.toolDescriptorOverrides, toolDescriptorOverride{
			name: name,
			fn:   fn,
		})
		return nil
	}
}

// applyToolDescriptorOverrides applies the configured patches to descriptors
// already registered in the tool registry. Called once after all capabilities
// have run RegisterTools, before the conversation marks capability registration
// complete.
//
// Each override gets a clone of the descriptor; if the patch mutates the
// clone, the new descriptor is re-registered (Registry.Register is
// last-write-wins, so the patched descriptor replaces the original).
func applyToolDescriptorOverrides(
	registry *tools.Registry, overrides []toolDescriptorOverride,
) {
	for _, ov := range overrides {
		desc := registry.Get(ov.name)
		if desc == nil {
			logger.Warn("tool descriptor override skipped: tool not registered",
				"name", ov.name)
			continue
		}
		patched := cloneToolDescriptor(desc)
		ov.fn(patched)
		if err := registry.Register(patched); err != nil {
			logger.Warn("tool descriptor override failed to re-register",
				"name", ov.name, "error", err)
			continue
		}
		logger.Debug("tool descriptor override applied", "name", ov.name)
	}
}

// cloneToolDescriptor returns a deep-enough copy of d so a patch fn can
// mutate fields without affecting the original. JSON-typed schema fields
// are cloned by byte-copy; slice fields are copied; pointer-typed fields
// are not deep-copied (the pipeline does not mutate them in practice).
func cloneToolDescriptor(d *tools.ToolDescriptor) *tools.ToolDescriptor {
	if d == nil {
		return nil
	}
	cp := *d
	if d.InputSchema != nil {
		cp.InputSchema = append([]byte(nil), d.InputSchema...)
	}
	if d.OutputSchema != nil {
		cp.OutputSchema = append([]byte(nil), d.OutputSchema...)
	}
	return &cp
}
