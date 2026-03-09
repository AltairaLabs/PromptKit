package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolFilter_Includes(t *testing.T) {
	t.Run("empty filter includes everything", func(t *testing.T) {
		f := ToolFilter{}
		assert.True(t, f.Includes("any_tool"))
		assert.True(t, f.Includes("another_tool"))
		assert.True(t, f.Includes(""))
	})

	t.Run("allowlist only includes listed names", func(t *testing.T) {
		f := ToolFilter{Allowlist: []string{"read", "write"}}
		assert.True(t, f.Includes("read"))
		assert.True(t, f.Includes("write"))
		assert.False(t, f.Includes("delete"))
		assert.False(t, f.Includes(""))
	})

	t.Run("blocklist excludes listed names", func(t *testing.T) {
		f := ToolFilter{Blocklist: []string{"dangerous", "internal"}}
		assert.True(t, f.Includes("read"))
		assert.True(t, f.Includes("write"))
		assert.False(t, f.Includes("dangerous"))
		assert.False(t, f.Includes("internal"))
	})

	t.Run("allowlist takes precedence over blocklist", func(t *testing.T) {
		f := ToolFilter{
			Allowlist: []string{"read", "write"},
			Blocklist: []string{"read", "delete"},
		}
		// "read" is in both — allowlist wins, so it's included
		assert.True(t, f.Includes("read"))
		assert.True(t, f.Includes("write"))
		// "delete" is in blocklist but not allowlist — excluded by allowlist logic
		assert.False(t, f.Includes("delete"))
		// not in either list — excluded by allowlist logic
		assert.False(t, f.Includes("other"))
	})
}
