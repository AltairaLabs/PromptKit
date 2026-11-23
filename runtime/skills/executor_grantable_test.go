package skills

import (
	"slices"
	"testing"
)

// GrantableTools is every tool a registered skill can grant, capped by the
// pack's tools, whether or not any skill is active or a set's filter admits
// it: the tools ToolsFor would return with every skill active.
func TestGrantableTools_EverySkillCappedByPack(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "refunds", "Refunds", "Refund.", []string{"refund", "not_in_pack"})
	writeSkillWithTools(t, dir, "shipping", "Shipping", "Ship.", []string{"track", "refund"})
	writeSkillWithTools(t, dir, "plain", "No tools", "Talk.", nil)
	exec := newTestExecutor(t, dir, []string{"lookup_order", "refund", "track"}, 0)

	got := exec.GrantableTools()

	if want := []string{"refund", "track"}; !slices.Equal(got, want) {
		t.Fatalf("GrantableTools() = %v, want %v", got, want)
	}
	set := NewActiveSet()
	set.SetFilter("none")
	if len(exec.ToolsFor(set)) != 0 {
		t.Fatal("nothing is active in the set")
	}
}
