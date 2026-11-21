package agui

import (
	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// Cost units (runtime/providers/base) whose counts AG-UI reports as named
// parts of a total that PromptKit's headline counts leave out.
const (
	unitCacheWriteToken = "cache_write_token" //nolint:gosec // a cost unit's name, not a credential
	unitReasoningToken  = "reasoning_token"
)

// unknownProvider labels the usage of a model call whose cost names no
// provider, so no entry goes out without one.
const unknownProvider = "unknown"

// runUsage totals the token usage of the model calls a run made, one entry
// per provider and model, in the order the run first used each.
type runUsage struct {
	order   []usageKey
	entries map[usageKey]*tokenTotals
}

// usageKey is what a usage entry is reported per: a provider and a model.
type usageKey struct{ provider, model string }

// tokenTotals is one provider's usage under AG-UI's accounting: input and
// output are totals, the cache and reasoning counts parts of them.
type tokenTotals struct {
	input, output, cacheRead, cacheWrite, reasoning int64
}

// addResponse adds the usage of every model call resp's turn made: the cost
// each of its messages records (an assistant reply's, or a tool's that called
// a model), or the reply's when the turn has no messages.
func (u *runUsage) addResponse(resp *sdk.Response) {
	if resp == nil {
		return
	}
	msgs := resp.TurnMessages()
	if len(msgs) == 0 {
		if m := resp.Message(); m != nil {
			u.add(m.CostInfo)
		}
		return
	}
	for i := range msgs {
		u.add(msgs[i].CostInfo)
	}
}

// add adds one model call's cost. PromptKit's headline counts leave cache
// writes out of input and reasoning out of output; AG-UI counts both in.
func (u *runUsage) add(c *types.CostInfo) {
	if c == nil {
		return
	}
	cacheWrite := int64(c.Quantities[unitCacheWriteToken])
	reasoning := int64(c.Quantities[unitReasoningToken])
	input := int64(c.InputTokens+c.CachedTokens) + cacheWrite
	output := int64(c.OutputTokens) + reasoning
	if input == 0 && output == 0 {
		return // nothing reported: no zero counts AG-UI would read as data
	}
	if u.entries == nil {
		u.entries = map[usageKey]*tokenTotals{}
	}
	key := usageKey{provider: c.ProviderName, model: c.Model}
	if key.provider == "" {
		key.provider = unknownProvider
	}
	e, ok := u.entries[key]
	if !ok {
		e = &tokenTotals{}
		u.entries[key] = e
		u.order = append(u.order, key)
	}
	e.input += input
	e.output += output
	e.cacheRead += int64(c.CachedTokens)
	e.cacheWrite += cacheWrite
	e.reasoning += reasoning
}

// tokenUsage is the run's usage for RUN_FINISHED or RUN_ERROR, nil when no
// call reported any. A cache or reasoning count is set only when it is
// nonzero: PromptKit records no zero for them, so an absent one means not
// reported, as AG-UI reads it.
func (u *runUsage) tokenUsage() []aguievents.TokenUsage {
	if len(u.order) == 0 {
		return nil
	}
	out := make([]aguievents.TokenUsage, 0, len(u.order))
	for _, key := range u.order {
		e := u.entries[key]
		tu := aguievents.TokenUsage{
			Provider:     key.provider,
			Model:        key.model,
			InputTokens:  aguievents.TokenCount(e.input),
			OutputTokens: aguievents.TokenCount(e.output),
			TotalTokens:  aguievents.TokenCount(e.input + e.output),
		}
		if e.reasoning > 0 {
			tu.ReasoningTokens = aguievents.TokenCount(e.reasoning)
		}
		if e.cacheRead > 0 {
			tu.CachedInputTokens = aguievents.TokenCount(e.cacheRead)
		}
		if e.cacheWrite > 0 {
			tu.CacheWriteInputTokens = aguievents.TokenCount(e.cacheWrite)
		}
		out = append(out, tu)
	}
	return out
}
