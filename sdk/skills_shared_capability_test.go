package sdk

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
)

// A SkillsCapability a host passes to several opens with WithCapability is
// shared. Skill sources added in code for one open belong to that conversation
// alone: they must reach it, must not leak into the others, and must not be
// written into the host's capability.

func hostSkillsCapability() *SkillsCapability {
	return NewSkillsCapability([]skills.SkillSource{{
		Name: "from-host", Description: "host skill", Instructions: "HOST-BODY", Preload: true,
	}})
}

func openWithSharedSkills(t *testing.T, sc *SkillsCapability, extra ...Option) string {
	t.Helper()
	provider := newSystemRecorder()
	opts := append([]Option{WithProvider(provider), WithSkipSchemaValidation(), WithCapability(sc)}, extra...)
	conv, err := Open(writeWorkflowTestPack(t, skillsPlainPackJSON), "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	_, err = conv.Send(context.Background(), "hello")
	require.NoError(t, err)
	return provider.firstSystem(t)
}

func TestSharedSkillsCapability_CodeSourcesStayWithTheirOpen(t *testing.T) {
	sc := hostSkillsCapability()

	first := openWithSharedSkills(t, sc, WithSkillSource(skills.SkillSource{
		Name: "code-a", Description: "a", Instructions: "CODE-A-BODY", Preload: true,
	}))
	assert.Contains(t, first, "HOST-BODY")
	assert.Contains(t, first, "CODE-A-BODY")

	plain := openWithSharedSkills(t, sc)
	assert.Contains(t, plain, "HOST-BODY")
	assert.NotContains(t, plain, "CODE-A-BODY", "another open's code source must not leak into this one")

	second := openWithSharedSkills(t, sc, WithSkillSource(skills.SkillSource{
		Name: "code-b", Description: "b", Instructions: "CODE-B-BODY", Preload: true,
	}))
	assert.Contains(t, second, "HOST-BODY")
	assert.Contains(t, second, "CODE-B-BODY", "a code source added after the first open must still apply")
	assert.NotContains(t, second, "CODE-A-BODY")

	assert.Equal(t, hostSkillsCapability().sources, sc.sources, "the host's capability must not be modified")
}

// Run with -race: concurrent opens sharing one capability must not race on it.
func TestSharedSkillsCapability_ConcurrentOpens(t *testing.T) {
	sc := hostSkillsCapability()
	path := writeWorkflowTestPack(t, skillsPlainPackJSON)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			opts := []Option{WithProvider(newSystemRecorder()), WithSkipSchemaValidation(), WithCapability(sc)}
			if i%2 == 0 {
				opts = append(opts, WithSkillSource(skills.SkillSource{
					Name: fmt.Sprintf("code-%d", i), Description: "d", Instructions: "x",
				}), WithSkillSelectorOption(nil), WithMaxActiveSkillsOption(3))
			}
			conv, err := Open(path, "chat", opts...)
			if err == nil {
				_ = conv.Close()
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, hostSkillsCapability().sources, sc.sources)
}

// openSharedIndex opens a conversation on sc, sends one message and returns the
// skill__activate description its first round offered.
func openSharedIndex(t *testing.T, sc *SkillsCapability, extra ...Option) string {
	t.Helper()
	provider := newSystemRecorder()
	opts := append([]Option{WithProvider(provider), WithSkipSchemaValidation(), WithCapability(sc)}, extra...)
	conv, err := Open(writeWorkflowTestPack(t, skillsPlainPackJSON), "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	_, err = conv.Send(context.Background(), "which skill?")
	require.NoError(t, err)
	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.NotEmpty(t, provider.descs)
	return provider.descs[0]
}

// withRuntimeSkillsSelector binds a selector the way a RuntimeConfig's
// spec.skills.selector does: registered by name, then named for skills.
func withRuntimeSkillsSelector(name string, selected ...string) []Option {
	return []Option{
		WithSelector(name, &capTestSelector{name: name, selected: selected}),
		func(c *config) error { c.skillsSelectorName = name; return nil },
	}
}

// Each open's RuntimeConfig selector narrows its own conversation's index, even
// when the capability is shared and was already built by an earlier open.
func TestSharedSkillsCapability_RuntimeSelectorIsPerOpen(t *testing.T) {
	sc := NewSkillsCapability([]skills.SkillSource{
		{Name: "alpha", Description: "a", Instructions: "A"},
		{Name: "beta", Description: "b", Instructions: "B"},
	})

	plain := openSharedIndex(t, sc)
	assert.Contains(t, plain, "- alpha:")
	assert.Contains(t, plain, "- beta:")

	chooseAlpha := openSharedIndex(t, sc, withRuntimeSkillsSelector("s", "alpha")...)
	assert.Contains(t, chooseAlpha, "- alpha:")
	assert.NotContains(t, chooseAlpha, "- beta:", "this open's selector chose alpha")

	chooseBeta := openSharedIndex(t, sc, withRuntimeSkillsSelector("s", "beta")...)
	assert.Contains(t, chooseBeta, "- beta:")
	assert.NotContains(t, chooseBeta, "- alpha:", "this open's selector chose beta")

	again := openSharedIndex(t, sc)
	assert.Contains(t, again, "- alpha:")
	assert.Contains(t, again, "- beta:", "an earlier open's selector must not stick to the shared capability")
}
