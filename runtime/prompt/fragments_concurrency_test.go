package prompt_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// One Registry is shared by concurrent conversations: PromptArena runs
// scenarios in parallel against one, and every conversation opened from an
// sdk.PackTemplate shares one. These tests render through a shared registry
// from goroutines released together, so the first writes to its caches race
// each other. Run them under -race.

var concurrencyRegions = []string{"us", "eu", "uk", "au", "jp", "br", "in", "za"}

const concurrentRenders = 32

func personaContent(region string) string {
	return "Persona for " + region + "."
}

// concurrencyRegistries returns, for each repository implementation, a cold
// registry holding the "support" prompt: "{{persona_{{region}}}}", whose
// fragment is chosen by the region variable, from one file per region.
func concurrencyRegistries(t *testing.T, tmpl string, ref func(f *sourceFormat) prompt.FragmentRef,
	fragments map[string]string,
) map[string]*prompt.Registry {
	t.Helper()
	regs := make(map[string]*prompt.Registry)
	for i := range sourceFormats {
		f := &sourceFormats[i]
		p := supportPrompt(tmpl, ref(f))
		p.variables = []string{"region"}
		regs[f.name] = f.load(t, sourceTree{prompts: []sourcePrompt{p}, fragments: fragments})
	}

	repo := memory.NewPromptRepository()
	cfg := &prompt.Config{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "PromptConfig",
		Spec: prompt.Spec{
			TaskType:       "support",
			Version:        "1.0.0",
			SystemTemplate: tmpl,
			Fragments:      []prompt.FragmentRef{{Name: ref(nil).Name, Required: true}},
			Variables:      []prompt.VariableMetadata{{Name: "region", Required: true}},
		},
	}
	repo.RegisterPrompt("support", cfg)
	for name, content := range fragments {
		repo.RegisterFragment(name, &prompt.Fragment{Type: "text", Content: content})
	}
	regs["memory"] = prompt.NewRegistryWithRepository(repo)
	return regs
}

// renderConcurrently renders "support" from concurrentRenders goroutines that
// start together, goroutine i with region concurrencyRegions[i%8], and returns
// each one's system prompt ("" for a failed render).
func renderConcurrently(reg *prompt.Registry) []string {
	results := make([]string, concurrentRenders)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range concurrentRenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			region := concurrencyRegions[i%len(concurrencyRegions)]
			if assembled := reg.LoadWithVars("support", map[string]string{"region": region}, ""); assembled != nil {
				results[i] = assembled.SystemPrompt
			}
		}()
	}
	close(start)
	wg.Wait()
	return results
}

// Every region's fragment is cold when the goroutines start, so they write the
// fragment cache concurrently (#2524).
func TestFragmentsConcurrency_ColdDynamicFragments(t *testing.T) {
	fragments := make(map[string]string, len(concurrencyRegions))
	for _, region := range concurrencyRegions {
		fragments["persona_"+region] = personaContent(region)
	}
	dynamicRef := func(f *sourceFormat) prompt.FragmentRef {
		ref := prompt.FragmentRef{Name: "persona_{{region}}", Required: true}
		if f != nil {
			ref.Path = f.fragPath("persona_{{region}}")
		}
		return ref
	}
	for name, reg := range concurrencyRegistries(t, "{{persona_{{region}}}}", dynamicRef, fragments) {
		t.Run(name, func(t *testing.T) {
			results := renderConcurrently(reg)
			for i, got := range results {
				region := concurrencyRegions[i%len(concurrencyRegions)]
				assert.Equal(t, personaContent(region), got, "goroutine %d (region %s)", i, region)
			}
		})
	}
}

// A static fragment loaded once before the goroutines start is only read
// after that; the read path must stay race-free too.
func TestFragmentsConcurrency_WarmStaticFragment(t *testing.T) {
	staticRef := func(f *sourceFormat) prompt.FragmentRef {
		ref := prompt.FragmentRef{Name: "tone", Required: true}
		if f != nil {
			ref.Path = f.fragPath("tone")
		}
		return ref
	}
	for name, reg := range concurrencyRegistries(t, "{{tone}} Region: {{region}}.", staticRef,
		map[string]string{"tone": toneContent}) {
		t.Run(name, func(t *testing.T) {
			warm := reg.LoadWithVars("support", map[string]string{"region": "us"}, "")
			require.NotNil(t, warm, "warming render failed")

			results := renderConcurrently(reg)
			for i, got := range results {
				region := concurrencyRegions[i%len(concurrencyRegions)]
				assert.Equal(t, fmt.Sprintf("%s Region: %s.", toneContent, region), got, "goroutine %d", i)
			}
		})
	}
}
