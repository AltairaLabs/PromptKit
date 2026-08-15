package adaptersdk

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

// Existence is what a probe could determine about a resource.
//
// Unknown is the zero value on purpose: a probe that fails to answer, or a
// caller that forgets to set one, must land on the conservative outcome rather
// than on "absent".
type Existence int

const (
	// ExistsUnknown means the lookup could not determine the answer. It is not
	// evidence of absence.
	ExistsUnknown Existence = iota
	// ExistsYes means the resource is present. It says nothing about health:
	// a degraded resource still exists, so it is an update, not a recreate.
	ExistsYes
	// ExistsNo means the resource is confirmed gone.
	ExistsNo
)

// ResourceRef identifies a resource recorded in prior state.
//
// Type and Name mirror deploy.ResourceChange so a dropped resource can be
// reported without translation. ID carries the provider's own identifier when
// the adapter stored one — often the only handle a lookup can use.
type ResourceRef struct {
	Type string `json:"type"`
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
}

// ResourceProbe answers whether a previously-deployed resource still exists.
//
// Adapters implement this against their own API; everything else about drift
// reconciliation is shared. Returning an error is equivalent to answering
// ExistsUnknown — the error is kept for the message, not for the decision.
type ResourceProbe interface {
	Exists(ctx context.Context, ref ResourceRef) (Existence, error)
}

// ReconcilePriorState drops resources that no longer exist and reports them as
// drift.
//
// A plan built from a stored state blob alone cannot see changes made outside
// promptarena: delete a resource in the cloud console and the adapter plans an
// UPDATE against something that is gone, which then fails at apply. Probing
// prior state against the live provider is what closes that gap.
//
// Three rules, and the third is the one worth sharing:
//
//   - confirmed absent  → drop, and report drift
//   - present, however degraded → keep; it exists, so it is an update
//   - lookup failed or unknown  → keep
//
// The last is easy to get backwards and expensive when wrong. Dropping a
// resource because the lookup errored plans a CREATE that collides with the
// live resource at apply time — turning a transient API error into a hard
// failure, or worse, a duplicate. Vertex makes this concrete: a malformed
// resource name returns InvalidArgument, not NotFound, so any "not found means
// absent" shortcut that treats every error alike deletes real state.
//
// A nil probe keeps everything: an adapter that cannot look up resources has
// no basis to drop them, and silently dropping would turn every plan into a
// full recreate.
func ReconcilePriorState(
	ctx context.Context, probe ResourceProbe, refs []ResourceRef,
) (kept []ResourceRef, drift []deploy.ResourceChange) {
	if len(refs) == 0 {
		return nil, nil
	}
	if probe == nil {
		return refs, nil
	}

	kept = make([]ResourceRef, 0, len(refs))
	for _, r := range refs {
		existence, err := probe.Exists(ctx, r)
		if err != nil {
			// Keep: the lookup failed, which tells us nothing about the
			// resource. See the rules above.
			kept = append(kept, r)
			continue
		}
		if existence == ExistsNo {
			drift = append(drift, driftChange(r))
			continue
		}
		kept = append(kept, r)
	}
	return kept, drift
}

// driftChange builds the ResourceChange reported for a resource that vanished.
//
// Drift is a ResourceChange rather than a free-text warning so it travels the
// same path as every other change — counted by SummarizeChanges, rendered in
// the plan, and machine-readable — instead of prose each adapter formats its
// own way.
func driftChange(r ResourceRef) deploy.ResourceChange {
	detail := fmt.Sprintf("%s no longer exists at the provider; it was removed outside promptarena", r.Name)
	if r.ID != "" {
		detail = fmt.Sprintf("%s (%s) no longer exists at the provider; it was removed outside promptarena",
			r.Name, r.ID)
	}
	return deploy.ResourceChange{
		Type:   r.Type,
		Name:   r.Name,
		Action: deploy.ActionDrift,
		Detail: detail,
	}
}
