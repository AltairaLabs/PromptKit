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
