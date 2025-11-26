// Package policy provides storage retention and cleanup policy management.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
)

// TimeBasedPolicyHandler implements PolicyHandler for time-based retention policies.
// It applies expiration times to media based on policy names and enforces deletion
// of expired media through background scanning.
type TimeBasedPolicyHandler struct {
	// policies maps policy names to their configurations
	policies map[string]Config

	// enforcementInterval is how often to scan for expired media
	enforcementInterval time.Duration

	// stopCh signals the enforcement goroutine to stop
	stopCh chan struct{}

	// doneCh signals when enforcement has finished
	doneCh chan struct{}
}

// NewTimeBasedPolicyHandler creates a new time-based policy handler.
func NewTimeBasedPolicyHandler(enforcementInterval time.Duration) *TimeBasedPolicyHandler {
	return &TimeBasedPolicyHandler{
		policies:            make(map[string]Config),
		enforcementInterval: enforcementInterval,
		stopCh:              make(chan struct{}),
		doneCh:              make(chan struct{}),
	}
}

// RegisterPolicy adds a policy configuration to the handler.
func (h *TimeBasedPolicyHandler) RegisterPolicy(policy Config) error {
	if err := policy.Validate(); err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}

	h.policies[policy.Name] = policy
	return nil
}

// ApplyPolicy implements storage.PolicyHandler.ApplyPolicy
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

// EnforcePolicy implements storage.PolicyHandler.EnforcePolicy
func (h *TimeBasedPolicyHandler) EnforcePolicy(ctx context.Context, baseDir string) error {
	now := time.Now()
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
		fmt.Printf("Policy enforcement: deleted %d files, encountered %d errors\n", deleted, errors)
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
func (h *TimeBasedPolicyHandler) StartEnforcement(ctx context.Context, baseDir string) {
	go func() {
		defer close(h.doneCh)

		ticker := time.NewTicker(h.enforcementInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := h.EnforcePolicy(ctx, baseDir); err != nil {
					fmt.Printf("Policy enforcement error: %v\n", err)
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
func (h *TimeBasedPolicyHandler) Stop() {
	close(h.stopCh)
	<-h.doneCh
}

// loadPolicyMetadata loads policy metadata from a .meta file.
func (h *TimeBasedPolicyHandler) loadPolicyMetadata(metaPath string) (*Metadata, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	// The .meta file contains MediaMetadata with PolicyName
	// We need to extract policy info and compute expiration
	var mediaMetadata storage.MediaMetadata
	if err := json.Unmarshal(data, &mediaMetadata); err != nil {
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
