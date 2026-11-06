// Package policy provides storage retention and cleanup policy management.
package policy

import (
	"context"
	"encoding/json"
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
// storage.MediaPolicyHandler and storage.MediaPolicyTracker.
var (
	_ storage.MediaPolicyHandler = (*TimeBasedPolicyHandler)(nil)
	_ storage.MediaPolicyTracker = (*TimeBasedPolicyHandler)(nil)
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

	// indexBuilt indicates whether the expiry index has been populated
	indexBuilt bool

	// beforeEnforceEntry, when set by a test, runs after enforceFromIndex has
	// taken its snapshot of expired entries and before it enforces each one,
	// so a test can interleave a concurrent TrackFile deterministically.
	beforeEnforceEntry func(metaPath string)

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
	errors := 0

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
			errors++
		}
	}

	if deleted > 0 || errors > 0 {
		logger.Info("Policy enforcement completed (indexed)", "deleted", deleted, "errors", errors)
	}

	return nil
}

// enforceEntry enforces one entry from enforceFromIndex's snapshot. It holds
// expiryMu throughout, so a TrackFile for the same path cannot land between
// the check that the entry is unchanged and the removal of its files and its
// index entry. An entry re-tracked or untracked since the snapshot is skipped:
// its files are left alone and a fresh entry is kept. It reports whether it
// enforced the entry and whether enforcing it failed.
func (h *TimeBasedPolicyHandler) enforceEntry(entry expiryEntry, now time.Time) (enforced, failed bool) {
	h.expiryMu.Lock()
	defer h.expiryMu.Unlock()

	if cur, ok := h.expiryIndex[entry.metaPath]; !ok || cur.gen != entry.gen {
		return false, false
	}
	if _, err := os.Stat(entry.metaPath); os.IsNotExist(err) {
		// Already deleted elsewhere; nothing left to enforce.
		delete(h.expiryIndex, entry.metaPath)
		return false, false
	}
	if !h.processMetaFile(entry.metaPath, now) {
		return false, true
	}
	delete(h.expiryIndex, entry.metaPath)
	return true, false
}

// enforceFromWalk is the original full directory walk implementation, used
// when no expiry index has been built.
func (h *TimeBasedPolicyHandler) enforceFromWalk(_ context.Context, baseDir string) error {
	now := h.now()
	deleted := 0
	errors := 0

	// Walk the base directory looking for .meta files
	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log and continue on errors (e.g., permission denied)
			errors++
			return nil
		}

		// Skip directories and non-.meta files
		if info.IsDir() || !strings.HasSuffix(path, ".meta") {
			return nil
		}

		// Process this .meta file
		if h.processMetaFile(path, now) {
			deleted++
		} else {
			errors++
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	// Log enforcement results
	if deleted > 0 || errors > 0 {
		logger.Info("Policy enforcement completed", "deleted", deleted, "errors", errors)
	}

	return nil
}

// processMetaFile checks if a .meta file's media has expired and deletes it if so.
// Returns true if deletion succeeded, false otherwise.
func (h *TimeBasedPolicyHandler) processMetaFile(metaPath string, now time.Time) bool {
	// Load metadata from .meta file
	metaData, err := h.loadPolicyMetadata(metaPath)
	if err != nil {
		// Skip files we can't read
		return false
	}

	// Check if expired
	if metaData.ExpiresAt == nil || !metaData.ExpiresAt.Before(now) {
		// Not expired or no expiration
		return true // Not an error, just nothing to do
	}

	// Delete the media file
	mediaPath := strings.TrimSuffix(metaPath, ".meta")
	if err := os.Remove(mediaPath); err != nil && !os.IsNotExist(err) {
		return false
	}

	// Delete the .meta file
	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return false
	}

	return true
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
// metadata file is at metaPath to the expiry index, expiring at
// metadata.Timestamp plus the policy's duration, so media stored after
// StartEnforcement has built the index still expires. Media with no policy is
// not tracked; a policy ParsePolicyName cannot parse is an error.
func (h *TimeBasedPolicyHandler) TrackMedia(metaPath string, metadata *storage.MediaMetadata) error {
	if metadata == nil || metadata.PolicyName == "" {
		return nil
	}
	_, duration, err := ParsePolicyName(metadata.PolicyName)
	if err != nil {
		return fmt.Errorf("failed to parse policy name: %w", err)
	}
	h.TrackFile(metaPath, metadata.Timestamp.Add(duration))
	return nil
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

// loadPolicyMetadata loads policy metadata from a .meta file.
func (h *TimeBasedPolicyHandler) loadPolicyMetadata(metaPath string) (*Metadata, error) {
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

	if mediaMetadata.PolicyName == "" {
		return nil, fmt.Errorf("no policy name in metadata")
	}

	// Parse policy to get duration
	_, duration, err := ParsePolicyName(mediaMetadata.PolicyName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse policy: %w", err)
	}

	// Calculate expiration time
	expiresAt := mediaMetadata.Timestamp.Add(duration)

	return &Metadata{
		PolicyName: mediaMetadata.PolicyName,
		ExpiresAt:  &expiresAt,
		CreatedAt:  mediaMetadata.Timestamp,
	}, nil
}
