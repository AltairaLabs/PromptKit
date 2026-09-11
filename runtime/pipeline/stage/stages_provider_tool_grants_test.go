package stage

import (
	"testing"
)

// These cover the provider-stage half of #1957: ProviderConfig.ToolGrants is a
// live accessor for tools granted beyond the prompt's baseline (active skills'
// allowed-tools), merged into the allowed list on every tools build.

func newGrantsTestStage(t *testing.T, grants func() []string) *ProviderStage {
	t.Helper()
	return &ProviderStage{
		toolRegistry: registryWithTools(t, "a", "b", "c"),
		config:       &ProviderConfig{ToolGrants: grants},
	}
}

func TestWithGrantedTools_NoAccessor_ReturnsInputUnchanged(t *testing.T) {
	s := &ProviderStage{config: &ProviderConfig{}}
	in := []string{"a", "b"}
	out := s.withGrantedTools(in)
	if len(out) != 2 || out[0] != "a" || out[1] != "b" {
		t.Fatalf("expected input unchanged, got %v", out)
	}
}

func TestWithGrantedTools_NilConfig_ReturnsInputUnchanged(t *testing.T) {
	s := &ProviderStage{}
	out := s.withGrantedTools([]string{"a"})
	if len(out) != 1 || out[0] != "a" {
		t.Fatalf("expected input unchanged, got %v", out)
	}
}

func TestWithGrantedTools_AppendsGrantsNotAlreadyAllowed(t *testing.T) {
	s := newGrantsTestStage(t, func() []string { return []string{"c", "b"} })
	in := []string{"a", "b"}
	out := s.withGrantedTools(in)

	if len(out) != 3 || out[0] != "a" || out[1] != "b" || out[2] != "c" {
		t.Fatalf("expected baseline then new grants, deduped: got %v", out)
	}
	if len(in) != 2 {
		t.Fatalf("input slice must not be mutated, got %v", in)
	}
}

func TestWithGrantedTools_EmptyGrantsLeavesBaseline(t *testing.T) {
	s := newGrantsTestStage(t, func() []string { return nil })
	out := s.withGrantedTools([]string{"a"})
	if len(out) != 1 || out[0] != "a" {
		t.Fatalf("expected baseline only, got %v", out)
	}
}

// TestGrantedTools_IsReadLive pins the property the mid-turn rebuild depends
// on: the accessor is consulted on every call, not snapshotted.
func TestGrantedTools_IsReadLive(t *testing.T) {
	current := []string{}
	s := newGrantsTestStage(t, func() []string { return current })

	if got := s.grantedTools(); len(got) != 0 {
		t.Fatalf("expected no grants yet, got %v", got)
	}
	current = []string{"c"}
	if got := s.grantedTools(); len(got) != 1 || got[0] != "c" {
		t.Fatalf("expected the new grant to be visible, got %v", got)
	}
}

func TestGrantedTools_SortedCopy(t *testing.T) {
	backing := []string{"c", "a"}
	s := newGrantsTestStage(t, func() []string { return backing })

	got := s.grantedTools()
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("expected sorted grants, got %v", got)
	}
	if backing[0] != "c" {
		t.Fatalf("accessor's slice must not be sorted in place, got %v", backing)
	}
}

// TestCollectProviderDescriptors_IncludesGrantedTool pins that a granted
// tool the prompt does not list reaches the descriptor list the provider is
// handed, while a granted name the registry does not know is dropped.
func TestCollectProviderDescriptors_IncludesGrantedTool(t *testing.T) {
	s := newGrantsTestStage(t, func() []string { return []string{"c", "not_registered"} })

	descriptors := s.collectProviderDescriptors(s.withGrantedTools([]string{"a"}), map[string]bool{})

	names := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		names = append(names, d.Name)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "c" {
		t.Fatalf("expected [a c], got %v", names)
	}
}

func TestStringSlicesEqual(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{nil, []string{}, true},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a"}, []string{"b"}, false},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a", "b"}, []string{"b", "a"}, false},
	}
	for _, c := range cases {
		if got := stringSlicesEqual(c.a, c.b); got != c.want {
			t.Errorf("stringSlicesEqual(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
