package evals

import (
	"fmt"
	"sync"
	"testing"
)

// The package-level default sets are written by init functions in other
// packages (handlers, exec plugins) and read whenever a registry is built or a
// type is classified, so they must tolerate concurrent use. Run with -race:
// without the lock these tests report a data race.

func TestDefaultRegistrations_ConcurrentUse(t *testing.T) {
	origHandlers, origAliases := defaultHandlers, defaultAliases
	t.Cleanup(func() {
		defaultsMu.Lock()
		defaultHandlers, defaultAliases = origHandlers, origAliases
		defaultsMu.Unlock()
	})

	const workers = 8
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(3)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("race_type_%d", i)
			RegisterDefault(&stubHandler{typeName: name})
			RegisterDefaultAlias(name+"_alias", name)
		}()
		go func() {
			defer wg.Done()
			_ = NewEvalTypeRegistry()
		}()
		go func() {
			defer wg.Done()
			_ = DefaultAliases()
		}()
	}
	wg.Wait()

	r := NewEvalTypeRegistry()
	for i := range workers {
		name := fmt.Sprintf("race_type_%d", i)
		if !r.Has(name) || !r.Has(name+"_alias") {
			t.Errorf("registry is missing %q or its alias", name)
		}
	}
}

func TestRegisterTypeGroups_ConcurrentUse(t *testing.T) {
	const workers = 8
	t.Cleanup(func() {
		typeGroupsMu.Lock()
		defer typeGroupsMu.Unlock()
		for i := range workers {
			delete(customTypeGroups, fmt.Sprintf("race_groups_%d", i))
		}
	})

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(2)
		name := fmt.Sprintf("race_groups_%d", i)
		go func() {
			defer wg.Done()
			RegisterTypeGroups(name, []string{GroupExternal})
		}()
		go func() {
			defer wg.Done()
			_ = DefaultGroupsForType(name)
		}()
	}
	wg.Wait()

	for i := range workers {
		got := DefaultGroupsForType(fmt.Sprintf("race_groups_%d", i))
		if len(got) != 2 || got[1] != GroupExternal {
			t.Errorf("groups = %v, want [%s %s]", got, DefaultEvalGroup, GroupExternal)
		}
	}
}

func TestRegisterTypeGroups_CopiesGroups(t *testing.T) {
	groups := []string{GroupLongRunning}
	RegisterTypeGroups("copy_groups_type", groups)
	t.Cleanup(func() {
		typeGroupsMu.Lock()
		delete(customTypeGroups, "copy_groups_type")
		typeGroupsMu.Unlock()
	})
	groups[0] = GroupExternal

	got := DefaultGroupsForType("copy_groups_type")
	if len(got) != 2 || got[1] != GroupLongRunning {
		t.Errorf("groups = %v: changing the caller's slice changed the registration", got)
	}
}
