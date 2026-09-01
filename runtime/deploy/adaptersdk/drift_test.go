package adaptersdk

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/deploy"
)

// fakeProbe answers from a scripted map, and records what it was asked.
type fakeProbe struct {
	answers map[string]Existence
	errs    map[string]error
	asked   []string
}

func (p *fakeProbe) Exists(_ context.Context, ref ResourceRef) (Existence, error) {
	p.asked = append(p.asked, ref.Name)
	if err, ok := p.errs[ref.Name]; ok {
		return ExistsUnknown, err
	}
	return p.answers[ref.Name], nil
}

func ref(name string) ResourceRef {
	return ResourceRef{Type: "agent_runtime", Name: name, ID: "id-" + name}
}

// A resource deleted outside promptarena must be dropped from prior state, or
// the plan plots an UPDATE against something that no longer exists and apply
// fails.
func TestReconcilePriorState_DropsAbsentResources(t *testing.T) {
	probe := &fakeProbe{answers: map[string]Existence{
		"alpha": ExistsYes,
		"beta":  ExistsNo,
	}}

	kept, drift := ReconcilePriorState(context.Background(), probe,
		[]ResourceRef{ref("alpha"), ref("beta")})

	require.Len(t, kept, 1)
	assert.Equal(t, "alpha", kept[0].Name)

	require.Len(t, drift, 1)
	assert.Equal(t, deploy.ActionDrift, drift[0].Action)
	assert.Equal(t, "beta", drift[0].Name)
	assert.Equal(t, "agent_runtime", drift[0].Type)
	assert.Contains(t, drift[0].Detail, "no longer exists")
}

// The rule that is easy to get backwards and expensive when wrong: a failed
// lookup is not evidence of absence. Dropping on error plans a CREATE that
// collides with the live resource at apply time.
func TestReconcilePriorState_KeepsResourcesWhoseLookupFailed(t *testing.T) {
	probe := &fakeProbe{
		answers: map[string]Existence{"alpha": ExistsYes},
		errs:    map[string]error{"beta": errors.New("InvalidArgument: malformed name")},
	}

	kept, drift := ReconcilePriorState(context.Background(), probe,
		[]ResourceRef{ref("alpha"), ref("beta")})

	require.Len(t, kept, 2, "a resource whose lookup errored must be kept")
	assert.Empty(t, drift, "an errored lookup is not drift — nothing was observed to be missing")
}

// ExistsUnknown without an error means the same thing: the probe could not
// tell, so the resource stays.
func TestReconcilePriorState_KeepsUnknown(t *testing.T) {
	probe := &fakeProbe{answers: map[string]Existence{"alpha": ExistsUnknown}}

	kept, drift := ReconcilePriorState(context.Background(), probe, []ResourceRef{ref("alpha")})

	require.Len(t, kept, 1)
	assert.Empty(t, drift)
}

// A resource that exists but is unhealthy is still an UPDATE, not a recreate,
// so the probe reporting ExistsYes must keep it regardless of its condition.
func TestReconcilePriorState_KeepsDegradedResources(t *testing.T) {
	probe := &fakeProbe{answers: map[string]Existence{"degraded": ExistsYes}}

	kept, drift := ReconcilePriorState(context.Background(), probe, []ResourceRef{ref("degraded")})

	require.Len(t, kept, 1)
	assert.Empty(t, drift)
}

// Without a probe the adapter has no way to check, so prior state is trusted
// as-is. Silently dropping everything would turn every plan into a full
// recreate.
func TestReconcilePriorState_NilProbeKeepsEverything(t *testing.T) {
	refs := []ResourceRef{ref("alpha"), ref("beta")}

	kept, drift := ReconcilePriorState(context.Background(), nil, refs)

	assert.Equal(t, refs, kept)
	assert.Empty(t, drift)
}

func TestReconcilePriorState_EmptyRefs(t *testing.T) {
	probe := &fakeProbe{answers: map[string]Existence{}}

	kept, drift := ReconcilePriorState(context.Background(), probe, nil)

	assert.Empty(t, kept)
	assert.Empty(t, drift)
	assert.Empty(t, probe.asked, "an empty prior state must not call the provider at all")
}

// Order is preserved so a plan built from the kept refs is deterministic.
func TestReconcilePriorState_PreservesOrder(t *testing.T) {
	probe := &fakeProbe{answers: map[string]Existence{
		"a": ExistsYes, "b": ExistsNo, "c": ExistsYes, "d": ExistsYes,
	}}

	kept, _ := ReconcilePriorState(context.Background(), probe,
		[]ResourceRef{ref("a"), ref("b"), ref("c"), ref("d")})

	require.Len(t, kept, 3)
	assert.Equal(t, []string{"a", "c", "d"}, []string{kept[0].Name, kept[1].Name, kept[2].Name})
}

// Drift is reported as a ResourceChange rather than a string so it flows into
// the plan the same way every other change does — countable, renderable and
// machine-readable, instead of prose each adapter formats differently.
func TestSummarizeChanges_RendersDrift(t *testing.T) {
	changes := []deploy.ResourceChange{
		{Type: "agent_runtime", Name: "a", Action: deploy.ActionCreate},
		{Type: "agent_runtime", Name: "b", Action: deploy.ActionDrift},
		{Type: "agent_runtime", Name: "c", Action: deploy.ActionDrift},
	}

	assert.Equal(t, "1 to create, 2 drifted", SummarizeChanges(changes))
}

// A plan that is nothing but drift must not read as "No changes" — that is the
// case an operator most needs to see.
func TestSummarizeChanges_DriftOnlyIsNotNoChanges(t *testing.T) {
	changes := []deploy.ResourceChange{
		{Type: "agent_runtime", Name: "b", Action: deploy.ActionDrift},
	}

	assert.Equal(t, "1 drifted", SummarizeChanges(changes))
}
