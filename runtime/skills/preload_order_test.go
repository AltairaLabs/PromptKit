package skills

import (
	"sort"
	"testing"
)

// preloadSourceSet returns inline sources whose declaration order is
// deliberately not their sorted order.
func preloadSourceSet() []SkillSource {
	names := []string{"zeta", "alpha", "mike", "bravo", "yankee", "charlie"}
	srcs := make([]SkillSource, 0, len(names))
	for _, n := range names {
		srcs = append(srcs, SkillSource{
			Name:         n,
			Description:  n + " skill",
			Instructions: "do " + n,
			Preload:      true,
		})
	}
	return srcs
}

// TestPreloadedSkills_OrderIsDeterministic guards the sort in
// PreloadedSkills. It is load-bearing: skills are held in a map, and when
// MaxActive is lower than the number of skills marked preload, the sort is
// the only thing that decides which ones get activated. Without it the
// active set would differ per process.
func TestPreloadedSkills_OrderIsDeterministic(t *testing.T) {
	want := []string{"alpha", "bravo", "charlie", "mike", "yankee", "zeta"}
	if !sort.StringsAreSorted(want) {
		t.Fatal("test fixture is not sorted")
	}

	// Rebuild repeatedly: one pass could agree with the expected order by
	// luck if the sort were removed.
	for i := 0; i < 10; i++ {
		reg := NewRegistry()
		if err := reg.Discover(preloadSourceSet()); err != nil {
			t.Fatalf("Discover: %v", err)
		}

		got := make([]string, 0, len(want))
		for _, sk := range reg.PreloadedSkills() {
			got = append(got, sk.Name)
		}

		if len(got) != len(want) {
			t.Fatalf("PreloadedSkills returned %d skills, want %d", len(got), len(want))
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("iteration %d: PreloadedSkills order = %v, want %v", i, got, want)
			}
		}
	}
}
