package policy_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage/policy"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// storeNamed stores a small image through fs under the named policy, stamped
// at the clock's current time, and returns its path on disk.
func storeNamed(
	t *testing.T, fs *local.FileStore, clock *movableClock, run, policyName string,
) (string, error) {
	t.Helper()
	data := base64.StdEncoding.EncodeToString([]byte("img-" + run))
	ref, err := fs.StoreMedia(context.Background(),
		&types.MediaContent{MIMEType: types.MIMETypeImagePNG, Data: &data},
		&storage.MediaMetadata{RunID: run, PolicyName: policyName, Timestamp: clock.now()})
	return string(ref), err
}

// TestRegisteredPolicy_DefinesRetention pins that a policy registered with
// RegisterPolicy decides the retention of media that name it, through a real
// FileStore and the handler's enforcement: a custom-named policy is accepted
// and expires media after its retention rule, and a registered rule overrides
// the duration a parseable name would give. An unregistered custom name is
// still refused.
func TestRegisteredPolicy_DefinesRetention(t *testing.T) {
	dir := t.TempDir()
	clock := &movableClock{}
	h := policy.NewTimeBasedPolicyHandler(2*time.Millisecond, policy.WithClock(clock.now))
	require.NoError(t, h.RegisterPolicy(policy.Config{
		Name: "gdpr-short", Description: "Keep for ten minutes",
		Rules: map[string]interface{}{"retention": "10m"},
	}))
	require.NoError(t, h.RegisterPolicy(policy.Config{
		Name:  "retain-30days",
		Rules: map[string]interface{}{"retention": "20m"},
	}))

	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir: dir, Organization: storage.OrganizationByRun, PolicyHandler: h,
	})
	require.NoError(t, err)

	_, err = storeNamed(t, fs, clock, "run-unknown", "gdpr-long")
	require.Error(t, err, "an unregistered custom policy name is refused")

	custom, err := storeNamed(t, fs, clock, "run-custom", "gdpr-short")
	require.NoError(t, err)
	override, err := storeNamed(t, fs, clock, "run-override", "retain-30days")
	require.NoError(t, err)

	expiry, expires, err := h.MediaExpiresAt(&storage.MediaMetadata{PolicyName: "gdpr-short", Timestamp: testNow})
	require.NoError(t, err)
	assert.True(t, expires)
	assert.Equal(t, testNow.Add(10*time.Minute), expiry)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.StartEnforcement(ctx, dir)
	defer h.Stop()

	clock.advance(11 * time.Minute)
	waitForNoFile(t, custom, 2*time.Second)
	assert.FileExists(t, override, "the registered 20m rule, not the name's 30 days, applies")

	clock.advance(10 * time.Minute)
	waitForNoFile(t, override, 2*time.Second)
}

func TestPolicyConfig_ValidateRetentionRule(t *testing.T) {
	tests := []struct {
		name    string
		config  policy.Config
		wantErr string
	}{
		{"custom name with retention", policy.Config{Name: "gdpr", Rules: map[string]interface{}{"retention": "90m"}}, ""},
		{"custom name without retention", policy.Config{Name: "gdpr"}, "invalid policy name"},
		{"retention not a duration", policy.Config{Name: "gdpr", Rules: map[string]interface{}{"retention": "soon"}}, "retention"},
		{"retention not positive", policy.Config{Name: "gdpr", Rules: map[string]interface{}{"retention": "0s"}}, "retention"},
		{"retention not a string", policy.Config{Name: "gdpr", Rules: map[string]interface{}{"retention": 5}}, "must be a duration string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestRegisterPolicy_ConcurrentWithLookups registers policies while other
// goroutines resolve media expiry against them, all released together; run
// under -race. Every lookup of a registered name sees its retention.
func TestRegisterPolicy_ConcurrentWithLookups(t *testing.T) {
	h := policy.NewTimeBasedPolicyHandler(time.Minute, policy.WithClock(testClock))
	require.NoError(t, h.RegisterPolicy(policy.Config{Name: "p", Rules: map[string]interface{}{"retention": "5m"}}))

	start := make(chan struct{})
	var wg sync.WaitGroup
	var wrong atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-start
			_ = h.RegisterPolicy(policy.Config{
				Name: fmt.Sprintf("extra-%d", i), Rules: map[string]interface{}{"retention": "1m"},
			})
		}(i)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 50; j++ {
				exp, _, err := h.MediaExpiresAt(&storage.MediaMetadata{PolicyName: "p", Timestamp: testNow})
				if err != nil || !exp.Equal(testNow.Add(5*time.Minute)) {
					wrong.Add(1)
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	assert.Zero(t, wrong.Load())

	exp, _, err := h.MediaExpiresAt(&storage.MediaMetadata{PolicyName: "extra-3", Timestamp: testNow})
	require.NoError(t, err)
	assert.Equal(t, testNow.Add(time.Minute), exp)
}
