package tools_test

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// These cover #1951. The registry held every descriptor twice — an eager
// preload copied the repository into the cache at construction, Register
// wrote through to both, and Get/List consulted the repository — so
// Unregister, which deleted from the cache only, was silently undone by the
// next Get or List. The repository is a loader consulted once at
// construction; after that the registry is the single store.

func descriptor(name string) *tools.ToolDescriptor {
	return &tools.ToolDescriptor{
		Name:        name,
		Description: "desc " + name,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

// The workflow terminal-state teardown: a descriptor registered after
// construction must be gone from every read path once unregistered.
func TestUnregister_RemovesFromEveryReadPath_WithRepository(t *testing.T) {
	repo := memory.NewToolRepository()
	repo.RegisterTool("pack_tool", descriptor("pack_tool"))
	registry := tools.NewRegistryWithRepository(repo)

	if err := registry.Register(descriptor("workflow__transition")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !registry.Unregister("workflow__transition") {
		t.Fatal("Unregister should report the descriptor was removed")
	}

	if got := registry.Get("workflow__transition"); got != nil {
		t.Errorf("Get resurrected an unregistered descriptor: %+v", got)
	}
	if _, err := registry.GetTool("workflow__transition"); err == nil {
		t.Error("GetTool should fail for an unregistered descriptor")
	}
	for _, name := range registry.List() {
		if name == "workflow__transition" {
			t.Errorf("List still reports an unregistered descriptor: %v", registry.List())
		}
	}
}

// A preloaded (pack) tool is unregisterable too — the store it came from is
// not consulted again.
func TestUnregister_RemovesPreloadedTool(t *testing.T) {
	repo := memory.NewToolRepository()
	repo.RegisterTool("pack_tool", descriptor("pack_tool"))
	registry := tools.NewRegistryWithRepository(repo)

	if !registry.Unregister("pack_tool") {
		t.Fatal("Unregister should remove a preloaded descriptor")
	}
	if registry.Get("pack_tool") != nil {
		t.Error("Get must not reload an unregistered descriptor from the repository")
	}
	if names := registry.List(); len(names) != 0 {
		t.Errorf("List must not read the repository; got %v", names)
	}
}

// Register no longer writes through: the repository is a loader, and a
// runtime-registered descriptor is not pack content.
func TestRegister_DoesNotWriteToRepository(t *testing.T) {
	repo := memory.NewToolRepository()
	registry := tools.NewRegistryWithRepository(repo)

	if err := registry.Register(descriptor("memory__remember")); err != nil {
		t.Fatalf("Register: %v", err)
	}

	names, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, name := range names {
		if name == "memory__remember" {
			t.Errorf("Register wrote through to the repository: %v", names)
		}
	}
	if registry.Get("memory__remember") == nil {
		t.Error("the registry itself must hold the registered descriptor")
	}
}

func TestRegister_NilDescriptorIsAnError(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(nil); err == nil {
		t.Fatal("Register(nil) must return an error, not panic or succeed")
	}
}

// List reports exactly what the registry holds — preloaded and registered —
// in a stable order, so callers that print it (warnings, prompts) are
// deterministic.
func TestList_ReflectsRegistryContentsSorted(t *testing.T) {
	repo := memory.NewToolRepository()
	repo.RegisterTool("zeta", descriptor("zeta"))
	repo.RegisterTool("alpha", descriptor("alpha"))
	registry := tools.NewRegistryWithRepository(repo)
	if err := registry.Register(descriptor("mid")); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got := registry.List()
	want := []string{"alpha", "mid", "zeta"}
	if !sort.StringsAreSorted(got) || len(got) != len(want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("List = %v, want %v", got, want)
		}
	}
}
