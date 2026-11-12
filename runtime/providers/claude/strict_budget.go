package claude

import (
	"encoding/json"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/schemaadapt"
)

// Anthropic compiles a grammar from every strict schema in a request (the
// strict tools and the JSON output schema) and refuses the request when they
// are too complex between them. These are the documented limits, each
// confirmed against api.anthropic.com: optional properties of nested objects
// and array items count, and non-strict tools do not.
const (
	maxStrictTools          = 20
	maxStrictOptionalParams = 24
	maxStrictUnionParams    = 16
)

// strictCost is what one schema spends of a request's strict budget.
type strictCost struct {
	optional int // properties not listed in their object's required
	unions   int // properties typed with anyOf or a type array
}

// strictCostOf counts a schema's optional and union-typed properties at every
// depth. A schema that does not decode costs nothing: the API reports it.
func strictCostOf(schema json.RawMessage) strictCost {
	var doc any
	if len(schema) == 0 || json.Unmarshal(schema, &doc) != nil {
		return strictCost{}
	}
	var c strictCost
	schemaadapt.Walk(doc, func(node map[string]any) {
		props, ok := node["properties"].(map[string]any)
		if !ok {
			return
		}
		required := requiredNames(node)
		for name, prop := range props {
			if !required[name] {
				c.optional++
			}
			if isUnionTyped(prop) {
				c.unions++
			}
		}
	})
	return c
}

// requiredNames is the set of property names an object node requires.
func requiredNames(node map[string]any) map[string]bool {
	required := map[string]bool{}
	list, _ := node["required"].([]any)
	for _, r := range list {
		if name, ok := r.(string); ok {
			required[name] = true
		}
	}
	return required
}

// isUnionTyped reports whether a property schema uses anyOf or a type array.
func isUnionTyped(prop any) bool {
	node, ok := prop.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := node["anyOf"]; ok {
		return true
	}
	_, isList := node["type"].([]any)
	return isList
}

// fitStrictTools keeps tools strict, in the order they were declared, while
// the request stays within Anthropic's strict limits, and sends any tool that
// would go over without strict and with the caller's schema. The output
// schema spends the budget first, since it is always constrained. Without
// this, a valid tool set (nine MCP tools with 27 optional parameters between
// them) fails every request with a 400.
func (p *ToolProvider) fitStrictTools(tools any, out *claudeOutputConfig) any {
	declared, ok := tools.([]claudeTool)
	if !ok {
		return tools
	}
	var spent strictCost
	if out != nil {
		spent = strictCostOf(out.Format.Schema)
	}
	strictCount := 0
	var demoted []string
	fitted := make([]claudeTool, len(declared))
	for i, t := range declared {
		fitted[i] = t
		if !t.Strict {
			continue
		}
		if strictCount+1 > maxStrictTools ||
			spent.optional+t.cost.optional > maxStrictOptionalParams ||
			spent.unions+t.cost.unions > maxStrictUnionParams {
			fitted[i].Strict = false
			fitted[i].InputSchema = t.callerSchema
			demoted = append(demoted, t.Name)
			continue
		}
		strictCount++
		spent.optional += t.cost.optional
		spent.unions += t.cost.unions
	}
	if len(demoted) > 0 {
		p.warnDemoted(demoted)
	}
	return fitted
}

// warnDemoted logs, once per provider for each set of tools, that strict tool
// use was dropped for them and why. A tool loop builds the same request every
// round, so a warning per request would repeat it for the whole conversation.
func (p *ToolProvider) warnDemoted(names []string) {
	key := strings.Join(names, ",")
	if _, seen := p.demotionsWarned.LoadOrStore(key, true); seen {
		return
	}
	logger.Warn("claude: sending tools without strict tool use; the request's strict schemas would exceed "+
		"Anthropic's limits (20 strict tools, 24 optional parameters, 16 union-typed parameters)",
		"provider", p.ID(), "tools", key)
}
