package evals

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// groupDeclaringHandler is an EvalTypeHandler that declares its own groups.
type groupDeclaringHandler struct {
	typeName string
	groups   []string
}

func (h *groupDeclaringHandler) Type() string { return h.typeName }

func (h *groupDeclaringHandler) Eval(context.Context, *EvalContext, map[string]any) (*EvalResult, error) {
	return &EvalResult{}, nil
}

func (h *groupDeclaringHandler) EvalGroups() []string { return h.groups }

func TestDefaultGroupsForType_RegisterDefaultUsesDeclaredGroups(t *testing.T) {
	RegisterDefault(&groupDeclaringHandler{typeName: "declared_default_type", groups: []string{GroupLongRunning}})
	assert.Equal(t, []string{DefaultEvalGroup, GroupLongRunning}, DefaultGroupsForType("declared_default_type"))
}

func TestDefaultGroupsForType_RegisterUsesDeclaredGroups(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	r.Register(&groupDeclaringHandler{typeName: "declared_instance_type", groups: []string{GroupExternal}})
	assert.Equal(t, []string{DefaultEvalGroup, GroupExternal}, DefaultGroupsForType("declared_instance_type"))
}

func TestDefaultGroupsForType_RegisterTypeGroupsWinsOverDeclared(t *testing.T) {
	RegisterDefault(&groupDeclaringHandler{typeName: "declared_and_custom", groups: []string{GroupLongRunning}})
	RegisterTypeGroups("declared_and_custom", []string{GroupFastRunning})
	assert.Equal(t, []string{DefaultEvalGroup, GroupFastRunning}, DefaultGroupsForType("declared_and_custom"))
}

func TestDefaultGroupsForType_DeclaredGroupsAreCopied(t *testing.T) {
	groups := []string{GroupLongRunning}
	RegisterDefault(&groupDeclaringHandler{typeName: "declared_copy_type", groups: groups})
	got := DefaultGroupsForType("declared_copy_type")
	got[1] = "mutated"
	assert.Equal(t, []string{DefaultEvalGroup, GroupLongRunning}, DefaultGroupsForType("declared_copy_type"))
}

func TestDefaultGroupsForType_ReplacedByNonDeclarer(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	r.Register(&groupDeclaringHandler{typeName: "declared_replaced_type", groups: []string{GroupLongRunning}})
	r.Register(&stubHandler{typeName: "declared_replaced_type"})
	assert.Equal(t, []string{DefaultEvalGroup, GroupFastRunning}, DefaultGroupsForType("declared_replaced_type"))
}

func TestDefaultGroupsForType_AliasOfDeclaredType(t *testing.T) {
	RegisterDefault(&groupDeclaringHandler{typeName: "declared_alias_target", groups: []string{GroupExternal}})
	RegisterDefaultAlias("declared_alias_name", "declared_alias_target")
	assert.Equal(t, []string{DefaultEvalGroup, GroupExternal}, DefaultGroupsForType("declared_alias_name"))
}

func TestGroups_WrapperWithoutInnerTypeUsesOwnType(t *testing.T) {
	def := &EvalDef{Type: WrapperTypeAssertion}
	assert.Equal(t, []string{DefaultEvalGroup, GroupFastRunning}, Groups(def))
}

func TestGroups_ExplicitGroupsOverrideWrapperInference(t *testing.T) {
	def := &EvalDef{
		Type:   WrapperTypeAssertion,
		Groups: []string{"mine"},
		Params: map[string]any{"eval_type": "llm_judge"},
	}
	assert.Equal(t, []string{"mine"}, Groups(def))
}
