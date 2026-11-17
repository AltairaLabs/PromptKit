package stage

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// offerRunner returns a tool runner over a registry holding names, under
// policy, configured by cfg.
func offerRunner(
	t *testing.T, policy *pipeline.ToolPolicy, cfg toolRunnerConfig, names ...string,
) *toolRunner {
	t.Helper()
	return &toolRunner{
		registry: registryWithTools(t, names...),
		policy:   func() *pipeline.ToolPolicy { return policy },
		cfg:      cfg,
		offers:   &toolOffer{},
	}
}

// offerNames returns the names of an offer, in order.
func offerNames(offer []*tools.ToolDescriptor) []string {
	names := make([]string, 0, len(offer))
	for _, d := range offer {
		names = append(names, d.Name)
	}
	return names
}

// The offer is the allowed tools plus the capability tools the registry holds,
// sorted by name; a tool neither allowed nor implicit is not offered.
func TestToolRunner_Offer_AllowedAndCapabilityToolsSorted(t *testing.T) {
	r := offerRunner(t, nil, toolRunnerConfig{},
		"refund", "get_order", "skill__activate", "unlisted", "mcp__kb__search")

	offer := r.offer([]string{"refund", "get_order"}, nil)

	require.Equal(t, []string{"get_order", "refund", "skill__activate"}, offerNames(offer))
	assert.Equal(t, "desc-get_order", offer[0].Description)
	assert.JSONEq(t, `{"type":"object"}`, string(offer[0].InputSchema))
}

// A tool the conversation's grants name is offered beyond the allowed list.
func TestToolRunner_Offer_GrantedToolAdded(t *testing.T) {
	r := offerRunner(t, nil, toolRunnerConfig{
		ToolGrants: func() []string { return []string{"refund"} },
	}, "get_order", "refund", "cancel")

	assert.Equal(t, []string{"get_order", "refund"}, offerNames(r.offer([]string{"get_order"}, nil)))
}

// An excluded tool is withheld whether it is allowed, granted or implicit.
func TestToolRunner_Offer_ExcludedRemoved(t *testing.T) {
	r := offerRunner(t, nil, toolRunnerConfig{
		ToolGrants: func() []string { return []string{"refund"} },
	}, "get_order", "refund", "skill__activate")

	offer := r.offer([]string{"get_order"}, map[string]bool{"refund": true, "skill__activate": true})

	assert.Equal(t, []string{"get_order"}, offerNames(offer))
}

// tool_choice none offers nothing at all, not even capability tools.
func TestToolRunner_Offer_ToolChoiceNoneOffersNothing(t *testing.T) {
	r := offerRunner(t, &pipeline.ToolPolicy{ToolChoice: toolChoiceNone}, toolRunnerConfig{},
		"get_order", "skill__activate")

	assert.Empty(t, r.offer([]string{"get_order"}, nil))
}

// DescribeTool shapes what is offered, and a nil from it withholds the tool.
func TestToolRunner_Offer_DescribeToolShapesAndWithholds(t *testing.T) {
	r := offerRunner(t, nil, toolRunnerConfig{
		DescribeTool: func(d *tools.ToolDescriptor) *tools.ToolDescriptor {
			if d.Name == "refund" {
				return nil
			}
			c := *d
			c.Description = "narrowed " + d.Name
			return &c
		},
	}, "get_order", "refund")

	offer := r.offer([]string{"get_order", "refund"}, nil)

	require.Equal(t, []string{"get_order"}, offerNames(offer))
	assert.Equal(t, "narrowed get_order", offer[0].Description)
}

// The offered record is shared state a realtime session writes from its
// session goroutine while its tool round reads it: under -race this fails
// without the lock.
func TestToolRunner_OfferedRecord_ConcurrentUse(t *testing.T) {
	r := offerRunner(t, nil, toolRunnerConfig{}, "get_order")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.recordOffered("get_order", "refund")
		}()
		go func() {
			defer wg.Done()
			msg := types.Message{}
			r.stampToolsOffered(&msg)
			r.resetOffered()
		}()
	}
	wg.Wait()
	r.recordOffered("refund")

	msg := types.Message{}
	r.stampToolsOffered(&msg)
	got, _ := msg.Meta[types.MetaToolsOffered].([]string)
	assert.Contains(t, got, "refund")
}

// Two runners built over the same offer state share its record, as the
// runners ProviderStage builds per use do.
func TestToolRunner_OfferedRecord_SharedAcrossRunners(t *testing.T) {
	state := &toolOffer{}
	first := &toolRunner{offers: state}
	first.recordOffered("refund")
	second := &toolRunner{offers: state}
	second.recordOffered("get_order")

	assert.Equal(t, []string{"get_order", "refund"}, first.offeredToolNames())
}

// An offer is a change only against the one last built: rebuilding the same
// array would bust the provider's cached prefix for nothing.
func TestToolRunner_DescribedChanged_ComparesToLastBuilt(t *testing.T) {
	view := "first view"
	r := offerRunner(t, nil, toolRunnerConfig{
		DescribeTool: func(d *tools.ToolDescriptor) *tools.ToolDescriptor {
			c := *d
			c.Description = view
			return &c
		},
	}, "get_order")

	built := r.offer([]string{"get_order"}, nil)
	assert.True(t, r.describedChanged(built), "nothing was built yet")
	r.markBuilt(built)
	assert.False(t, r.describedChanged(r.offer([]string{"get_order"}, nil)), "same view as last built")

	view = "second view"
	assert.True(t, r.describedChanged(r.offer([]string{"get_order"}, nil)), "DescribeTool's view moved")
}

// R3: a turn reads its grants from its runner. A grant that moves while the
// round's calls run is an offer change, with no wiring beyond newTurn.
func TestToolTurn_RunRound_GrantsChangeReportsOfferChanged(t *testing.T) {
	calls := 0
	runner := turnRunner(t, &pipeline.ToolPolicy{Blocklist: []string{"danger"}})
	runner.cfg.ToolGrants = func() []string {
		calls++
		if calls == 1 {
			return nil
		}
		return []string{"refund"}
	}
	turn := runner.newTurn(nil, 10)

	out := turn.RunRound(context.Background(), []types.MessageToolCall{dangerCall("c1")}, roundRef{round: 1})

	assert.Equal(t, roundOutcome{
		Results:      []types.Message{policyRefusal("c1", "danger", "Tool danger is blocked by policy")},
		OfferChanged: true,
	}, out)
}

// The grants a turn compares are a sorted copy: the same set in another order
// is no change.
func TestToolTurn_RunRound_GrantsReorderedIsNoChange(t *testing.T) {
	calls := 0
	runner := turnRunner(t, &pipeline.ToolPolicy{Blocklist: []string{"danger"}})
	runner.cfg.ToolGrants = func() []string {
		calls++
		if calls == 1 {
			return []string{"b", "a"}
		}
		return []string{"a", "b"}
	}

	out := runner.newTurn(nil, 10).RunRound(context.Background(),
		[]types.MessageToolCall{{ID: "c1", Name: "danger", Args: json.RawMessage(`{}`)}}, roundRef{round: 1})

	assert.False(t, out.OfferChanged)
}

// offeredAmong resolves exactly the names the offer would hold: allowed,
// wildcard-matched, granted and capability tools, less what DescribeTool
// withholds; a tool neither selected nor kept is not offered.
func TestToolRunner_OfferedAmongMatchesTheOffer(t *testing.T) {
	names := []string{"refund", "get_order", "skill__activate", "unlisted", "mcp__kb__search", "mcp__fs__read", "hidden"}
	r := offerRunner(t, nil, toolRunnerConfig{
		ToolGrants: func() []string { return []string{"get_order"} },
		DescribeTool: func(d *tools.ToolDescriptor) *tools.ToolDescriptor {
			if d.Name == "hidden" {
				return nil
			}
			return d
		},
	}, names...)
	allowed := []string{"refund", "hidden", "mcp__kb__*"}

	got := r.offeredAmong(allowed, names)

	want := map[string]bool{}
	for _, n := range offerNames(r.offer(allowed, nil)) {
		want[n] = true
	}
	for _, n := range names {
		assert.Equal(t, want[n], got[n], "tool %s", n)
	}
	assert.Equal(t, []string{"get_order", "mcp__kb__search", "refund", "skill__activate"},
		offerNames(r.offer(allowed, nil)))
}
