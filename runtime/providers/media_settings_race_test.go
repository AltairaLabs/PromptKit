package providers

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// One provider shared by conversations opened concurrently has its media
// settings written by each Open while others read them; under -race this
// fails without mediaSettingsMu.
func TestBaseProvider_MediaSettingsAreSafeForConcurrentUse(t *testing.T) {
	b := NewBaseProvider("shared", false, nil)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			b.SetAllowPrivateNetworkMedia(i%2 == 0)
			b.SetMediaStorageService(nil)
		}()
		go func() {
			defer wg.Done()
			_ = b.MediaLoader()
		}()
	}
	wg.Wait()
	b.SetAllowPrivateNetworkMedia(true)
	assert.True(t, b.allowPrivateMediaURLs)
}
