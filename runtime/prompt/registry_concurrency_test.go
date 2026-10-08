package prompt

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Conversations opened concurrently from one PackTemplate share a registry,
// and the repository hands every caller the same *Config. Loading it on a
// cold cache must not race on that config; under -race this failed before
// loadConfig populated it under the write lock.
func TestRegistry_ConcurrentColdLoadsDoNotRace(t *testing.T) {
	repo := newMockRepository()
	shared := &Config{Spec: Spec{TaskType: "p", SystemTemplate: "x", Validators: []ValidatorConfig{{}}}}
	repo.prompts["p"] = shared
	reg := NewRegistryWithRepository(repo)

	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = reg.CallParameters("p", "m")
		}()
	}
	wg.Wait()

	assert.Equal(t, "v1", shared.Spec.TemplateEngine.Version, "defaults populated once")
	assert.True(t, *shared.Spec.Validators[0].Enabled)
}
