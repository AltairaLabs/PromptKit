package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// CacheWriteTokens reads the cache writes a call's Quantities carry, and is
// zero for a call that reported none and for no cost at all.
func TestCostInfo_CacheWriteTokens(t *testing.T) {
	var none *CostInfo
	assert.Equal(t, 0, none.CacheWriteTokens())
	assert.Equal(t, 0, (&CostInfo{InputTokens: 9}).CacheWriteTokens())
	assert.Equal(t, 1250, (&CostInfo{Quantities: map[string]float64{"cache_write_token": 1250}}).CacheWriteTokens())
}
