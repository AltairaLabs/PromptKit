package handlers

import (
	"context"
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// TestTopicPolicyHandler_WarnKeyIsPolicyNotSession pins the dedup key's
// identity. It used to be evalCtx.SessionID, which the guardrail path never
// populates (BuildGuardrailEvalContext sets no SessionID), so every call keyed
// on "" and the handler — a process-wide singleton — warned exactly once for
// the life of the process. This asserts the key is derived from the policy
// digest and classifier id instead: two policies warn separately, and the same
// policy warns once however many turns run through it.
func TestTopicPolicyHandler_WarnKeyIsPolicyNotSession(t *testing.T) {
	h := &TopicPolicyHandler{}

	a := topicPolicyConfig{policy: classify.TopicPolicy{
		Description: "products", Allowed: []string{"omnia"},
	}}
	b := topicPolicyConfig{policy: classify.TopicPolicy{
		Description: "billing", Allowed: []string{"invoices"},
	}}
	aOtherClassifier := a
	aOtherClassifier.classifierID = "second"

	// Drive the real call path, not markWarned directly: warnUnbound is where
	// the key is built, and building it from the wrong thing is the bug.
	if _, seen := h.warnedGuardrails[warnKey(topicPolicyDigest(a.policy), "")]; seen {
		t.Fatal("tracker must start empty")
	}
	_ = h.warnUnbound(a, "no classify registry configured")
	_ = h.warnUnbound(a, "no classify registry configured")
	_ = h.warnUnbound(b, "no classify registry configured")
	_ = h.warnUnbound(aOtherClassifier, "no classify registry configured")

	if got := len(h.warnedGuardrails); got != 3 {
		t.Fatalf("warnedGuardrails has %d entries, want 3 (policy a, policy b, policy a + explicit classifier)", got)
	}
	for _, cfg := range []topicPolicyConfig{a, b, aOtherClassifier} {
		key := warnKey(topicPolicyDigest(cfg.policy), cfg.classifierID)
		if _, seen := h.warnedGuardrails[key]; !seen {
			t.Fatalf("no tracker entry for policy digest %q classifier %q",
				topicPolicyDigest(cfg.policy), cfg.classifierID)
		}
	}

	// And the key is NOT the session id: the eval context carrying one changes
	// nothing, because the handler never reads it.
	before := len(h.warnedGuardrails)
	_ = h.warnUnbound(a, "no classify registry configured")
	if got := len(h.warnedGuardrails); got != before {
		t.Fatalf("repeat warning for the same policy added an entry: %d -> %d", before, got)
	}
}

// TestTopicPolicyHandler_MarkWarnedFirstOccurrenceOnly pins "loud once per
// misconfigured guardrail, not once per turn": the first call for a given key
// reports true (log it), every later call for the same key reports false.
func TestTopicPolicyHandler_MarkWarnedFirstOccurrenceOnly(t *testing.T) {
	h := &TopicPolicyHandler{}

	if !h.markWarned("k1") {
		t.Fatal("first call for a key must report true")
	}
	if h.markWarned("k1") {
		t.Fatal("second call for the same key must report false")
	}
	if !h.markWarned("k2") {
		t.Fatal("first call for a different key must report true")
	}
}

// TestTopicPolicyHandler_MarkWarnedBoundsMemory is the #1996-follow-up
// regression test: TopicPolicyHandler is registered once as a process-wide
// singleton, so without a cap warnedGuardrails would accumulate one entry per
// distinct policy digest for the life of the process. Driving more than
// maxWarnedGuardrails distinct keys through it must never let the tracker grow
// past the cap. This asserts on the tracker's size directly rather than on log
// output — a test that only counted log lines would pass against an unbounded
// map too.
func TestTopicPolicyHandler_MarkWarnedBoundsMemory(t *testing.T) {
	h := &TopicPolicyHandler{}

	for i := 0; i < maxWarnedGuardrails*3; i++ {
		h.markWarned(fmt.Sprintf("policy-%d", i))
		if got := len(h.warnedGuardrails); got > maxWarnedGuardrails {
			t.Fatalf("warnedGuardrails has %d entries after %d distinct keys, want <= %d (cap)",
				got, i+1, maxWarnedGuardrails)
		}
	}
}

// TestTopicPolicyHandler_UnboundWarnsPerPolicyAcrossConversations is the
// behavioral half of the same fix, driven through Eval. With no registry on the
// context every turn errors, and the old session-keyed tracker logged for the
// first turn of the process only. Here each distinct policy is reported once and
// each repeat is suppressed, independent of how many conversations ran.
func TestTopicPolicyHandler_UnboundWarnsPerPolicyAcrossConversations(t *testing.T) {
	h := &TopicPolicyHandler{}
	params := func(desc string) map[string]any {
		return map[string]any{"description": desc, "allowed": []any{"omnia"}}
	}
	// Two separate conversations; neither carries a SessionID, because the
	// guardrail path does not set one.
	convA := &evals.EvalContext{CurrentOutput: "does Omnia run on OpenShift?"}
	convB := &evals.EvalContext{CurrentOutput: "how do I pay an invoice?"}

	// Conversation 1, policy A: first sighting, warns.
	if _, err := h.Eval(context.Background(), convA, params("policy A")); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(h.warnedGuardrails) != 1 {
		t.Fatalf("first turn recorded %d entries, want 1", len(h.warnedGuardrails))
	}
	// Conversation 2 (a fresh conversation, same process), policy B: a
	// session-keyed tracker would already be saturated and record nothing.
	if _, err := h.Eval(context.Background(), convB, params("policy B")); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(h.warnedGuardrails) != 2 {
		t.Fatalf("a second distinct policy recorded %d entries, want 2", len(h.warnedGuardrails))
	}
}
