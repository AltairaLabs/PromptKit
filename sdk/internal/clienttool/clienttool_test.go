package clienttool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeSettler is a Settler that reports rewriters.
type fakeSettler struct{ Settler }

func (fakeSettler) HasToolResultRewriters() bool { return true }

// SettlerOf asks the registered lookup, and is nil without one.
func TestSettlerOf_UsesTheRegisteredLookup(t *testing.T) {
	saved := settlerOf
	t.Cleanup(func() { settlerOf = saved })

	settlerOf = nil
	assert.Nil(t, SettlerOf("conv"))

	var asked []any
	Register(func(conv any) Settler {
		asked = append(asked, conv)
		if conv == "conv" {
			return fakeSettler{}
		}
		return nil
	})
	got := SettlerOf("conv")
	assert.Equal(t, fakeSettler{}, got)
	assert.True(t, got.HasToolResultRewriters())
	assert.Nil(t, SettlerOf("other"))
	assert.Equal(t, []any{"conv", "other"}, asked)
}
