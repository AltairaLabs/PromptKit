package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestA2ASkillFilter_IncludesSkill(t *testing.T) {
	t.Run("nil filter includes everything", func(t *testing.T) {
		var f *A2ASkillFilter
		assert.True(t, f.IncludesSkill("any_skill"))
		assert.True(t, f.IncludesSkill(""))
	})

	t.Run("empty filter includes everything", func(t *testing.T) {
		f := &A2ASkillFilter{}
		assert.True(t, f.IncludesSkill("any_skill"))
		assert.True(t, f.IncludesSkill("another_skill"))
		assert.True(t, f.IncludesSkill(""))
	})

	t.Run("allowlist only includes listed skills", func(t *testing.T) {
		f := &A2ASkillFilter{Allowlist: []string{"forecast", "alerts"}}
		assert.True(t, f.IncludesSkill("forecast"))
		assert.True(t, f.IncludesSkill("alerts"))
		assert.False(t, f.IncludesSkill("debug"))
		assert.False(t, f.IncludesSkill(""))
	})

	t.Run("blocklist excludes listed skills", func(t *testing.T) {
		f := &A2ASkillFilter{Blocklist: []string{"debug", "internal"}}
		assert.True(t, f.IncludesSkill("forecast"))
		assert.True(t, f.IncludesSkill("alerts"))
		assert.False(t, f.IncludesSkill("debug"))
		assert.False(t, f.IncludesSkill("internal"))
	})

	t.Run("allowlist takes precedence over blocklist", func(t *testing.T) {
		f := &A2ASkillFilter{
			Allowlist: []string{"forecast", "alerts"},
			Blocklist: []string{"forecast", "debug"},
		}
		// "forecast" is in both — allowlist wins
		assert.True(t, f.IncludesSkill("forecast"))
		assert.True(t, f.IncludesSkill("alerts"))
		// not in allowlist — rejected regardless of blocklist
		assert.False(t, f.IncludesSkill("debug"))
		assert.False(t, f.IncludesSkill("other"))
	})
}
