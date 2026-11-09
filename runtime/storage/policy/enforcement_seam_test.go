package policy_test

import (
	"context"
	"encoding/base64"
	"os"
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

// movableClock is a test clock the test can advance while enforcement runs.
type movableClock struct{ offset atomic.Int64 }

func (c *movableClock) now() time.Time          { return testNow.Add(time.Duration(c.offset.Load())) }
func (c *movableClock) advance(d time.Duration) { c.offset.Add(int64(d)) }

// storeWithPolicy stores a small image through a real FileStore wired to h,
// stamped at the clock's current time, and returns its path on disk.
func storeWithPolicy(t *testing.T, fs *local.FileStore, clock *movableClock, run string) string {
	t.Helper()
	data := base64.StdEncoding.EncodeToString([]byte("img-" + run))
	ref, err := fs.StoreMedia(context.Background(),
		&types.MediaContent{MIMEType: types.MIMETypeImagePNG, Data: &data},
		&storage.MediaMetadata{RunID: run, PolicyName: "delete-after-1min", Timestamp: clock.now()})
	require.NoError(t, err)
	require.FileExists(t, string(ref))
	return string(ref)
}

func waitForNoFile(t *testing.T, path string, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%s still exists after %v", path, deadline)
}

// TestEnforcement_DeletesMediaStoredThroughFileStore runs the handler's
// background enforcement against media a real FileStore stored and tracked:
// once the policy's time passes, the file is deleted; a second
// StartEnforcement does not start a second loop; after Stop, nothing is
// deleted any more.
func TestEnforcement_DeletesMediaStoredThroughFileStore(t *testing.T) {
	dir := t.TempDir()
	clock := &movableClock{}
	h := policy.NewTimeBasedPolicyHandler(2*time.Millisecond, policy.WithClock(clock.now))
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir: dir, Organization: storage.OrganizationByRun, PolicyHandler: h,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.StartEnforcement(ctx, dir)
	h.StartEnforcement(ctx, dir) // a second loop would close doneCh twice and Stop would panic

	expiring := storeWithPolicy(t, fs, clock, "run-a")
	time.Sleep(20 * time.Millisecond)
	assert.FileExists(t, expiring, "not deleted before its policy time")

	clock.advance(2 * time.Minute)
	waitForNoFile(t, expiring, 2*time.Second)
	assert.NoFileExists(t, expiring+".meta")

	require.NotPanics(t, func() {
		h.Stop()
		h.Stop()
	})

	afterStop := storeWithPolicy(t, fs, clock, "run-b")
	clock.advance(2 * time.Minute)
	time.Sleep(30 * time.Millisecond)
	assert.FileExists(t, afterStop, "enforcement must not run after Stop")
}

// TestStop_BeforeStartIsSafe pins that Stop on a handler that never started
// enforcement returns and may be repeated.
func TestStop_BeforeStartIsSafe(t *testing.T) {
	h := policy.NewTimeBasedPolicyHandler(5*time.Millisecond, policy.WithClock(testClock))
	done := make(chan struct{})
	go func() {
		h.Stop()
		h.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked on a handler that never started")
	}
}
