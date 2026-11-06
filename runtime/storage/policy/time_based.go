// Package policy provides storage retention and cleanup policy management.
package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
)

// Option configures a TimeBasedPolicyHandler.
type Option func(*TimeBasedPolicyHandler)

// WithAutoStart controls whether the background enforcement goroutine starts
// automatically when the handler is created. Requires WithBaseDir to be set.
// Default is false for backward compatibility with NewTimeBasedPolicyHandler.
func WithAutoStart(autoStart bool) Option {
	return func(h *TimeBasedPolicyHandler) {
		h.autoStart = autoStart
	}
}

// WithBaseDir sets the base directory for automatic policy enforcement.
// This is required when WithAutoStart(true) is used.
func WithBaseDir(baseDir string) Option {
	return func(h *TimeBasedPolicyHandler) {
		h.baseDir = baseDir
	}
}

// WithClock sets the function the handler uses to read the current time when
// deciding whether media has expired. It defaults to time.Now; tests inject a
// fixed clock so expiry does not depend on the wall clock.
func WithClock(now func() time.Time) Option {
	return func(h *TimeBasedPolicyHandler) {
		if now != nil {
			h.now = now
		}
	}
}

// expiryEntry tracks a file and its expiration time in the in-memory index.
type expiryEntry struct {
	metaPath  string
	expiresAt time.Time
	// gen changes every time the path is tracked or re-indexed, so the index
	// pass can tell that an entry it snapshotted has since been replaced.
	gen uint64
}

// compile-time checks that TimeBasedPolicyHandler implements
// storage.MediaPolicyHandler, storage.MediaPolicyTracker,
// storage.MediaWriteGuard and storage.MediaExpiryReporter.
var (
	_ storage.MediaPolicyHandler  = (*TimeBasedPolicyHandler)(nil)
	_ storage.MediaPolicyTracker  = (*TimeBasedPolicyHandler)(nil)
	_ storage.MediaWriteGuard     = (*TimeBasedPolicyHandler)(nil)
	_ storage.MediaExpiryReporter = (*TimeBasedPolicyHandler)(nil)
)

// TimeBasedPolicyHandler implements storage.MediaPolicyHandler for time-based retention policies.
// It applies expiration times to media based on policy names and enforces deletion
// of expired media through background scanning.
//
// When an expiry index is built (via BuildExpiryIndex, which StartEnforcement
// calls), enforcement uses the index to check only files whose expiry time has
// passed, avoiding a full directory walk on every tick. TrackFile and
// TrackMedia keep a built index current but never build it themselves.
type TimeBasedPolicyHandler struct {
	// policies maps policy names to their configurations
	policies map[string]Config

	// enforcementInterval is how often to scan for expired media
	enforcementInterval time.Duration

	// baseDir is the directory to enforce policies in (used for auto-start)
	baseDir string

	// now returns the current time; time.Now unless overridden by WithClock.
	now func() time.Time

	// autoStart controls whether enforcement starts automatically
	autoStart bool

	// stopCh signals the enforcement goroutine to stop
	stopCh chan struct{}

	// doneCh signals when enforcement has finished
	doneCh chan struct{}

	// started tracks whether enforcement has been started
	started bool

	// stopped tracks whether Stop has been called
	stopped bool

	// mu protects started and stopped fields
	mu sync.Mutex

	// expiryIndex holds files with their expiry times, keyed by meta path.
	// When populated, EnforcePolicy uses this index instead of walking the
	// entire directory tree.
	expiryIndex map[string]expiryEntry
	expiryMu    sync.RWMutex

	// nextGen is the last generation handed to an index entry; guarded by
	// expiryMu.
	nextGen uint64

	// writing counts the open BeginMediaWrite calls per metadata path, keyed
	// by writeKey; guarded by expiryMu. Enforcement skips those paths.
	writing map[string]int

	// enforcing holds, per writeKey, a channel for each item an enforcement
	// step is handling; it is closed when the step ends. Guarded by expiryMu,
	// which a step releases for its disk I/O. BeginMediaWrite waits for it, and
	// other enforcement steps skip the item.
	enforcing map[string]chan struct{}

	// indexBuilt indicates whether the expiry index has been populated
	indexBuilt bool

	// beforeEnforceEntry, when set by a test, runs after enforceFromIndex has
	// taken its snapshot of expired entries and before it enforces each one,
	// so a test can interleave a concurrent TrackFile deterministically.
	beforeEnforceEntry func(metaPath string)

	// beforeEnforceIO, when set by a test, runs before each disk operation an
	// enforcement step makes on an item: "stat", "read" or "remove".
	beforeEnforceIO func(op, path string)

	// waitForEnforcement, when set by a test, runs when BeginMediaWrite starts
	// waiting for an enforcement step handling its path.
	waitForEnforcement func(metaPath string)

	// cancel stops the auto-started enforcement goroutine. Set only when
	// autoStart is true; nil otherwise. Call Stop() to invoke it.
	cancel context.CancelFunc
}

// NewTimeBasedPolicyHandler creates a new time-based policy handler.
// It accepts optional configuration via Option functions.
// To auto-start enforcement, use WithAutoStart(true) and WithBaseDir(dir).
func NewTimeBasedPolicyHandler(enforcementInterval time.Duration, opts ...Option) *TimeBasedPolicyHandler {
	h := &TimeBasedPolicyHandler{
		policies:            make(map[string]Config),
		enforcementInterval: enforcementInterval,
		stopCh:              make(chan struct{}),
		doneCh:              make(chan struct{}),
		expiryIndex:         make(map[string]expiryEntry),
		now:                 time.Now,
	}

	for _, opt := range opts {
		opt(h)
	}

	if h.autoStart && h.baseDir != "" && h.enforcementInterval > 0 {
		var ctx context.Context
		ctx, h.cancel = context.WithCancel(context.Background())
		h.StartEnforcement(ctx, h.baseDir)
	}

	return h
}

// RegisterPolicy adds a policy configuration to the handler.
func (h *TimeBasedPolicyHandler) RegisterPolicy(policy Config) error {
	if err := policy.Validate(); err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.policies[policy.Name] = policy
	return nil
}

// ApplyPolicy implements storage.MediaPolicyHandler.ApplyPolicy. It returns an
// error when metadata names a policy that ParsePolicyName cannot parse, so a
// store using the handler rejects media whose retention it could never
// enforce. An empty policy name is accepted.
func (h *TimeBasedPolicyHandler) ApplyPolicy(ctx context.Context, metadata *storage.MediaMetadata) error {
	if metadata.PolicyName == "" {
		return nil // No policy to apply
	}

	// Parse the policy name to get duration
	_, duration, err := ParsePolicyName(metadata.PolicyName)
	if err != nil {
		return fmt.Errorf("failed to parse policy name: %w", err)
	}

	// Calculate expiration time (for validation purposes)
	_ = metadata.Timestamp.Add(duration)

	// Store policy metadata in the metadata object for persistence
	// The caller (e.g., LocalFileStore) will persist this to .meta file
	// The expiration time will be calculated by EnforcePolicy when needed

	return nil
}

// EnforcePolicy implements storage.MediaPolicyHandler.EnforcePolicy.
// When an expiry index has been built by BuildExpiryIndex (which
// StartEnforcement calls), this method only checks files whose expiry time
// has passed, avoiding a full directory walk. Otherwise it walks baseDir, and
// also enforces any files recorded with TrackFile or TrackMedia, so they do
// not accumulate in the index.
func (h *TimeBasedPolicyHandler) EnforcePolicy(ctx context.Context, baseDir string) error {
	h.expiryMu.RLock()
	indexed := h.indexBuilt
	h.expiryMu.RUnlock()

	if indexed {
		return h.enforceFromIndex(ctx)
	}
	if err := h.enforceFromIndex(ctx); err != nil {
		return err
	}
	return h.enforceFromWalk(ctx, baseDir)
}

// enforceFromIndex checks only files in the expiry index whose time has passed.
func (h *TimeBasedPolicyHandler) enforceFromIndex(_ context.Context) error {
	now := h.now()
	deleted := 0
	failures := 0

	// Collect expired entries under read lock
	h.expiryMu.RLock()
	var expired []expiryEntry
	for _, entry := range h.expiryIndex {
		if entry.expiresAt.Before(now) {
			expired = append(expired, entry)
		}
	}
	h.expiryMu.RUnlock()

	// Process expired entries
	for _, entry := range expired {
		if h.beforeEnforceEntry != nil {
			h.beforeEnforceEntry(entry.metaPath)
		}
		ok, failed := h.enforceEntry(entry, now)
		if ok {
			deleted++
		}
		if failed {
			failures++
		}
	}

	if deleted > 0 || failures > 0 {
		logger.Info("Policy enforcement completed (indexed)", "deleted", deleted, "errors", failures)
	}

	return nil
}

// diskOutcome is what an enforcement step found, and did, on disk.
type diskOutcome int

const (
	diskGone       diskOutcome = iota // the .meta was already gone
	diskNoPolicy                      // the .meta names no policy and no expiry: never expires
	diskNotExpired                    // not expired yet
	diskDeleted                       // expired, and its files were deleted
	diskFailed                        // unreadable, or a delete failed
)

// enforceEntry enforces one entry from enforceFromIndex's snapshot. Under
// expiryMu it checks that the entry is unchanged and no store is writing the
// item, and claims the item; it then releases the lock for the disk I/O, and
// takes it again to commit the result to the index. A store's BeginMediaWrite
// for the item waits for the claim, so a guarded write cannot land between the
// check and the deletion, while stores of other items are not held up. An
// entry re-tracked or untracked since the snapshot, before or during the I/O,
// keeps its fresh entry. It reports whether it enforced the entry and whether
// enforcing it failed.
func (h *TimeBasedPolicyHandler) enforceEntry(entry expiryEntry, now time.Time) (enforced, failed bool) {
	h.expiryMu.Lock()
	if !h.unchangedLocked(entry) {
		h.expiryMu.Unlock()
		return false, false
	}
	release, claimed := h.claimLocked(entry.metaPath)
	h.expiryMu.Unlock()
	if !claimed {
		// A store is replacing these files, or another pass is enforcing them;
		// the entry is retried next pass.
		return false, false
	}

	outcome, expiresAt := h.enforceClaimed(entry.metaPath, now, release)

	h.expiryMu.Lock()
	defer h.expiryMu.Unlock()
	release()
	if h.unchangedLocked(entry) {
		switch outcome {
		case diskGone, diskNoPolicy, diskDeleted:
			// Nothing left to enforce: deleted here or elsewhere, or the .meta
			// was rewritten with no policy and no expiry, and the media never expires.
			delete(h.expiryIndex, entry.metaPath)
		case diskNotExpired:
			// The .meta was rewritten with a later expiry without a re-track;
			// keep enforcing it at the expiry on disk rather than dropping it.
			entry.expiresAt = expiresAt
			h.expiryIndex[entry.metaPath] = entry
		case diskFailed:
			// Kept, and retried next pass.
		}
	}
	return outcome == diskDeleted, outcome == diskFailed
}

// unchangedLocked reports whether the index still holds entry with the
// generation it was snapshotted with. The caller holds expiryMu.
func (h *TimeBasedPolicyHandler) unchangedLocked(entry expiryEntry) bool {
	cur, ok := h.expiryIndex[entry.metaPath]
	return ok && cur.gen == entry.gen
}

// enforceWalkedFile enforces the .meta file the directory walk found at
// metaPath. Like enforceEntry, it skips a path a store is writing and claims
// the path under expiryMu, so a store cannot open its write guard for it
// between the check and the deletion, then does the disk I/O with the lock
// released. It reports whether it deleted the file and whether that failed.
func (h *TimeBasedPolicyHandler) enforceWalkedFile(metaPath string, now time.Time) (enforced, failed bool) {
	h.expiryMu.Lock()
	release, claimed := h.claimLocked(metaPath)
	h.expiryMu.Unlock()
	if !claimed {
		return false, false
	}

	outcome, _ := h.enforceClaimed(metaPath, now, release)

	h.expiryMu.Lock()
	release()
	h.expiryMu.Unlock()
	return outcome == diskDeleted, outcome == diskFailed
}

// claimLocked claims the item at metaPath for an enforcement step, unless a
// store is writing it or another step has claimed it. The returned function
// ends the claim; the caller calls it with expiryMu held. The caller holds
// expiryMu.
func (h *TimeBasedPolicyHandler) claimLocked(metaPath string) (release func(), claimed bool) {
	key := writeKey(metaPath)
	if h.beingWrittenLocked(metaPath) || h.enforcing[key] != nil {
		return nil, false
	}
	if h.enforcing == nil {
		h.enforcing = make(map[string]chan struct{})
	}
	done := make(chan struct{})
	h.enforcing[key] = done
	return func() {
		delete(h.enforcing, key)
		close(done)
	}, true
}

// enforceClaimed runs enforceOnDisk for an item the caller has claimed. If the
// disk I/O panics, it ends the claim before the panic propagates, so writers
// waiting in BeginMediaWrite for the item are not left blocked forever.
func (h *TimeBasedPolicyHandler) enforceClaimed(
	metaPath string, now time.Time, release func(),
) (diskOutcome, time.Time) {
	defer func() {
		if r := recover(); r != nil {
			h.expiryMu.Lock()
			release()
			h.expiryMu.Unlock()
			panic(r)
		}
	}()
	return h.enforceOnDisk(metaPath, now)
}

// enforceOnDisk reads the .meta at metaPath and, when its media has expired,
// deletes the media and the .meta. It takes no lock; the caller has claimed
// the item. For diskNotExpired it also returns the expiry on disk.
func (h *TimeBasedPolicyHandler) enforceOnDisk(metaPath string, now time.Time) (diskOutcome, time.Time) {
	h.enforceIO("stat", metaPath)
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		return diskGone, time.Time{}
	}
	meta, err := h.loadPolicyMetadata(metaPath)
	switch {
	case errors.Is(err, errNoPolicy):
		return diskNoPolicy, time.Time{}
	case err != nil:
		return diskFailed, time.Time{}
	case meta.ExpiresAt == nil:
		return diskNoPolicy, time.Time{}
	case !meta.ExpiresAt.Before(now):
		return diskNotExpired, *meta.ExpiresAt
	}

	mediaPath := strings.TrimSuffix(metaPath, ".meta")
	h.enforceIO("remove", mediaPath)
	if err := os.Remove(mediaPath); err != nil && !os.IsNotExist(err) {
		return diskFailed, time.Time{}
	}
	h.enforceIO("remove", metaPath)
	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return diskFailed, time.Time{}
	}
	return diskDeleted, time.Time{}
}

// BeginMediaWrite implements storage.MediaWriteGuard. Until the returned
// function is called, neither the index pass nor the directory walk deletes
// the item whose metadata file is at metaPath. It waits for an enforcement
// step already handling that path to finish, but not for steps handling other
// items. Overlapping writes to one path nest, and the returned function is
// safe to call more than once.
func (h *TimeBasedPolicyHandler) BeginMediaWrite(metaPath string) func() {
	key := writeKey(metaPath)
	h.expiryMu.Lock()
	for done := h.enforcing[key]; done != nil; done = h.enforcing[key] {
		h.expiryMu.Unlock()
		if h.waitForEnforcement != nil {
			h.waitForEnforcement(metaPath)
		}
		<-done
		h.expiryMu.Lock()
	}
	if h.writing == nil {
		h.writing = make(map[string]int)
	}
	h.writing[key]++
	h.expiryMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			h.expiryMu.Lock()
			defer h.expiryMu.Unlock()
			if h.writing[key] <= 1 {
				delete(h.writing, key)
				return
			}
			h.writing[key]--
		})
	}
}

// beingWrittenLocked reports whether a store holds a write guard for
// metaPath. The caller holds expiryMu.
func (h *TimeBasedPolicyHandler) beingWrittenLocked(metaPath string) bool {
	return h.writing[writeKey(metaPath)] > 0
}

// writeKey normalizes a metadata path so a guard opened by a store and a path
// found by the directory walk match even when one was built from a relative
// base directory and the other from an absolute one.
func writeKey(metaPath string) string {
	if abs, err := filepath.Abs(metaPath); err == nil {
		return abs
	}
	return filepath.Clean(metaPath)
}

// enforceFromWalk is the original full directory walk implementation, used
// when no expiry index has been built.
func (h *TimeBasedPolicyHandler) enforceFromWalk(_ context.Context, baseDir string) error {
	now := h.now()
	deleted := 0
	failures := 0

	// Walk the base directory looking for .meta files
	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log and continue on errors (e.g., permission denied)
			failures++
			return nil
		}

		// Skip directories and non-.meta files
		if info.IsDir() || !strings.HasSuffix(path, ".meta") {
			return nil
		}

		// Process this .meta file
		enforced, failed := h.enforceWalkedFile(path, now)
		if enforced {
			deleted++
		}
		if failed {
			failures++
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	// Log enforcement results
	if deleted > 0 || failures > 0 {
		logger.Info("Policy enforcement completed", "deleted", deleted, "errors", failures)
	}

	return nil
}

// StartEnforcement starts a background goroutine that periodically enforces policies.
// On the first call it builds an in-memory expiry index by scanning the base directory
// once, then uses that index on subsequent ticks to avoid repeated full directory walks.
func (h *TimeBasedPolicyHandler) StartEnforcement(ctx context.Context, baseDir string) {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return
	}
	h.started = true
	h.mu.Unlock()

	// Build the expiry index once at startup
	if err := h.BuildExpiryIndex(baseDir); err != nil {
		logger.Warn("Failed to build expiry index, falling back to directory walk", "error", err)
	}

	go func() {
		defer close(h.doneCh)

		ticker := time.NewTicker(h.enforcementInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := h.EnforcePolicy(ctx, baseDir); err != nil {
					logger.Error("Policy enforcement failed", "error", err)
				}
			case <-h.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop signals the enforcement goroutine to stop and waits for it to finish.
// It is safe to call Stop multiple times or when enforcement was never started.
func (h *TimeBasedPolicyHandler) Stop() {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	h.stopped = true
	wasStarted := h.started
	h.mu.Unlock()

	if h.cancel != nil {
		h.cancel()
	}

	close(h.stopCh)
	if wasStarted {
		<-h.doneCh
	}
}

// BuildExpiryIndex scans the base directory once and builds an in-memory
// index of files with their expiry times. Subsequent calls to EnforcePolicy
// will use this index instead of walking the directory tree.
func (h *TimeBasedPolicyHandler) BuildExpiryIndex(baseDir string) error {
	index := make(map[string]expiryEntry)

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() || !strings.HasSuffix(path, ".meta") {
			return nil
		}

		meta, loadErr := h.loadPolicyMetadata(path)
		if loadErr != nil {
			return nil // skip unreadable files
		}

		if meta.ExpiresAt != nil {
			index[path] = expiryEntry{
				metaPath:  path,
				expiresAt: *meta.ExpiresAt,
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to build expiry index: %w", err)
	}

	h.expiryMu.Lock()
	for path, entry := range index {
		h.nextGen++
		entry.gen = h.nextGen
		index[path] = entry
	}
	// Keep entries tracked before or during the scan. Replacing the map
	// outright would lose media stored while it ran, and a tracked entry is at
	// least as fresh as what the scan read, since a store tracks its files
	// after writing them. Entries whose files are gone are pruned by
	// enforceFromIndex.
	for path, entry := range h.expiryIndex {
		index[path] = entry
	}
	h.expiryIndex = index
	h.indexBuilt = true
	h.expiryMu.Unlock()

	return nil
}

// TrackFile adds a file to the in-memory expiry index. This should be called
// when a new file with a retention policy is stored, to keep the index current
// without requiring a full directory scan. Tracking a file does not switch
// EnforcePolicy to index-only mode; only BuildExpiryIndex does.
func (h *TimeBasedPolicyHandler) TrackFile(metaPath string, expiresAt time.Time) {
	h.expiryMu.Lock()
	defer h.expiryMu.Unlock()

	h.nextGen++
	h.expiryIndex[metaPath] = expiryEntry{
		metaPath:  metaPath,
		expiresAt: expiresAt,
		gen:       h.nextGen,
	}
	// Tracking never marks the index as built: the index holds only what was
	// tracked since the handler was created, so switching EnforcePolicy to
	// index-only mode here would strand expired media already on disk. Only
	// BuildExpiryIndex, which scans the whole directory, does that.
}

// TrackMedia implements storage.MediaPolicyTracker. It adds the media whose
// metadata file is at metaPath to the expiry index, expiring when
// MediaExpiresAt says, so media stored after StartEnforcement has built the
// index still expires. Media with no policy and no ExpiresAt is not tracked; a
// policy ParsePolicyName cannot parse is an error.
func (h *TimeBasedPolicyHandler) TrackMedia(metaPath string, metadata *storage.MediaMetadata) error {
	expiresAt, expires, err := mediaExpiry(metadata)
	if err != nil || !expires {
		return err
	}
	h.TrackFile(metaPath, expiresAt)
	return nil
}

// MediaExpiresAt implements storage.MediaExpiryReporter: media expires at its
// ExpiresAt when that is set, with or without a policy, and otherwise at its
// Timestamp plus its policy's duration. Media with neither never expires; a
// policy ParsePolicyName cannot parse is an error.
func (h *TimeBasedPolicyHandler) MediaExpiresAt(metadata *storage.MediaMetadata) (time.Time, bool, error) {
	return mediaExpiry(metadata)
}

// mediaExpiry returns when media with metadata expires, as MediaExpiresAt
// documents.
func mediaExpiry(metadata *storage.MediaMetadata) (time.Time, bool, error) {
	if metadata == nil {
		return time.Time{}, false, nil
	}
	var duration time.Duration
	if metadata.PolicyName != "" {
		var err error
		if _, duration, err = ParsePolicyName(metadata.PolicyName); err != nil {
			return time.Time{}, false, fmt.Errorf("failed to parse policy name: %w", err)
		}
	}
	switch {
	case metadata.ExpiresAt != nil:
		return *metadata.ExpiresAt, true, nil
	case metadata.PolicyName == "":
		return time.Time{}, false, nil
	default:
		return metadata.Timestamp.Add(duration), true, nil
	}
}

// UntrackFile removes a file from the in-memory expiry index. This should be
// called when a file is explicitly deleted, to keep the index current.
func (h *TimeBasedPolicyHandler) UntrackFile(metaPath string) {
	h.expiryMu.Lock()
	defer h.expiryMu.Unlock()

	delete(h.expiryIndex, metaPath)
}

// ExpiryIndexLen returns the number of entries in the expiry index.
// This is primarily useful for testing.
func (h *TimeBasedPolicyHandler) ExpiryIndexLen() int {
	h.expiryMu.RLock()
	defer h.expiryMu.RUnlock()
	return len(h.expiryIndex)
}

// errNoPolicy is returned by loadPolicyMetadata for a .meta that names no
// policy and sets no expiry: media that never expires.
var errNoPolicy = errors.New("no policy name or expiry in metadata")

// loadPolicyMetadata loads policy metadata from a .meta file.
func (h *TimeBasedPolicyHandler) loadPolicyMetadata(metaPath string) (*Metadata, error) {
	h.enforceIO("read", metaPath)
	data, err := os.ReadFile(filepath.Clean(metaPath))
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	// The .meta file contains MediaMetadata with PolicyName
	// We need to extract policy info and compute expiration
	var mediaMetadata storage.MediaMetadata
	if err = json.Unmarshal(data, &mediaMetadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	expiresAt, expires, err := mediaExpiry(&mediaMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse policy: %w", err)
	}
	if !expires {
		return nil, errNoPolicy
	}

	return &Metadata{
		PolicyName: mediaMetadata.PolicyName,
		ExpiresAt:  &expiresAt,
		CreatedAt:  mediaMetadata.Timestamp,
	}, nil
}

// enforceIO runs the beforeEnforceIO test hook, when set, before a disk
// operation of an enforcement step.
func (h *TimeBasedPolicyHandler) enforceIO(op, path string) {
	if h.beforeEnforceIO != nil {
		h.beforeEnforceIO(op, path)
	}
}
