package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordIOLockState sets h's IO hook to record each disk operation an
// enforcement step makes, with whether expiryMu was free at that moment.
func recordIOLockState(h *TimeBasedPolicyHandler) *[]string {
	var ops []string
	h.beforeEnforceIO = func(op, _ string) {
		state := "held"
		if h.expiryMu.TryLock() {
			h.expiryMu.Unlock()
			state = "free"
		}
		ops = append(ops, op+":"+state)
	}
	return &ops
}

// The index pass reads and deletes an item's files without holding expiryMu,
// so stores taking that lock to track or guard other media are not held up
// behind enforcement's disk I/O.
func TestEnforceFromIndex_DoesNoDiskIOUnderTheLock(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	ops := recordIOLockState(h)

	require.NoError(t, h.EnforcePolicy(context.Background(), filepath.Dir(media)))

	assert.NoFileExists(t, media)
	assert.Contains(t, *ops, "read:free")
	assert.Contains(t, *ops, "remove:free")
	for _, op := range *ops {
		assert.Contains(t, op, ":free", "disk I/O must run with expiryMu released")
	}
	assert.Equal(t, 0, h.ExpiryIndexLen())
}

// The directory walk does the same.
func TestEnforceFromWalk_DoesNoDiskIOUnderTheLock(t *testing.T) {
	h := NewTimeBasedPolicyHandler(0, WithClock(func() time.Time { return restoreNow }))
	media := filepath.Join(t.TempDir(), "walked.jpg")
	writeRestoreMedia(t, media, restoreNow.Add(-6*time.Minute))
	ops := recordIOLockState(h)

	require.NoError(t, h.EnforcePolicy(context.Background(), filepath.Dir(media)))

	assert.NoFileExists(t, media)
	assert.Contains(t, *ops, "read:free")
	assert.Contains(t, *ops, "remove:free")
	for _, op := range *ops {
		assert.Contains(t, op, ":free", "disk I/O must run with expiryMu released")
	}
}

// A TrackFile that lands while the index pass is doing an entry's disk I/O
// keeps its fresh entry: the pass re-checks the generation before it commits.
func TestEnforceEntry_TrackDuringIOKeepsFreshEntry(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	fresh := restoreNow.Add(10 * time.Minute)
	tracked := false
	h.beforeEnforceIO = func(op, _ string) {
		if op == "read" && !tracked {
			tracked = true
			h.TrackFile(media+".meta", fresh)
		}
	}

	require.NoError(t, h.EnforcePolicy(context.Background(), filepath.Dir(media)))

	require.True(t, tracked)
	at, ok := h.indexedExpiry(media + ".meta")
	require.True(t, ok, "the fresh entry stays in the index")
	assert.Equal(t, fresh, at)
}

// BeginMediaWrite for an item waits for an enforcement step already handling
// that item, so a store never writes into files enforcement is deleting.
func TestBeginMediaWrite_WaitsForEnforcementOfItsPath(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	reading, release := make(chan struct{}), make(chan struct{})
	paused := false
	h.beforeEnforceIO = func(op, _ string) {
		if op == "read" && !paused {
			paused = true
			close(reading)
			<-release
		}
	}
	waiting := make(chan struct{})
	h.waitForEnforcement = func(string) { close(waiting) }

	enforced := make(chan error)
	go func() { enforced <- h.EnforcePolicy(context.Background(), filepath.Dir(media)) }()
	<-reading

	opened := make(chan bool)
	go func() {
		end := h.BeginMediaWrite(media + ".meta")
		defer end()
		opened <- fileExists(media)
	}()

	select {
	case <-waiting:
	case <-opened:
		t.Fatal("BeginMediaWrite returned while enforcement was handling its path")
	}
	close(release)
	require.NoError(t, <-enforced)
	assert.False(t, <-opened, "the guard opens only after enforcement deleted the files")
}

// BeginMediaWrite for another item does not wait for an enforcement step.
func TestBeginMediaWrite_OtherPathDoesNotWaitForEnforcement(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	h.waitForEnforcement = func(string) { t.Error("a write to another item waited for enforcement") }
	h.beforeEnforceIO = func(op, _ string) {
		if op == "read" {
			h.BeginMediaWrite(filepath.Join(filepath.Dir(media), "other.jpg.meta"))()
		}
	}
	require.NoError(t, h.EnforcePolicy(context.Background(), filepath.Dir(media)))
	assert.NoFileExists(t, media)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// An item another enforcement step has claimed is skipped, files and entry
// alike, and enforced on a later pass.
func TestEnforceEntry_SkipsItemAnotherStepHolds(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	h.expiryMu.Lock()
	release, claimed := h.claimLocked(media + ".meta")
	h.expiryMu.Unlock()
	require.True(t, claimed)

	enforced, failed := h.enforceEntry(h.snapshotEntry(t, media+".meta"), restoreNow)
	assert.False(t, enforced)
	assert.False(t, failed)
	assert.FileExists(t, media)
	assert.Equal(t, 1, h.ExpiryIndexLen())

	h.expiryMu.Lock()
	release()
	h.expiryMu.Unlock()
	require.NoError(t, h.EnforcePolicy(context.Background(), filepath.Dir(media)))
	assert.NoFileExists(t, media)
}

// A panic during an enforcement step's disk I/O ends its claim, so a later
// write to the item does not wait forever.
func TestEnforceEntry_PanicDuringIOEndsTheClaim(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	h.beforeEnforceIO = func(op, _ string) {
		if op == "read" {
			panic("disk exploded")
		}
	}
	h.waitForEnforcement = func(string) { t.Error("the claim outlived the panicking step") }
	assert.Panics(t, func() { _ = h.EnforcePolicy(context.Background(), filepath.Dir(media)) })

	h.BeginMediaWrite(media + ".meta")()
}
