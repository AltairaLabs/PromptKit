package storage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MediaStorageService defines the interface for storing and retrieving media content.
// Implementations may store media in local filesystem, cloud storage, or other backends.
//
// Example usage:
//
//	storage := local.NewFileStore("/var/promptkit/media")
//	ref, err := storage.StoreMedia(ctx, mediaContent, metadata)
//	if err != nil {
//	    return err
//	}
//	// Later...
//	content, err := storage.RetrieveMedia(ctx, ref)
//
// Implementations should be safe for concurrent use by multiple goroutines.
type MediaStorageService interface {
	// StoreMedia stores media content and returns a storage reference.
	// The reference can be used to retrieve the media later.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - content: The media content to store (must have Data, FilePath, or URL set)
	//   - metadata: Metadata about the media for organization and policies
	//
	// Returns:
	//   - StorageReference that can be used to retrieve the media
	//   - Error if storage fails
	//
	// The implementation should:
	//   - Validate the content and metadata
	//   - Store the media content durably
	//   - Apply any configured policies (e.g., retention)
	//   - Return a reference that uniquely identifies the stored media
	StoreMedia(ctx context.Context, content *types.MediaContent, metadata *MediaMetadata) (StorageReference, error)

	// RetrieveMedia retrieves media content by its storage reference.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - reference: The storage reference returned by StoreMedia
	//
	// Returns:
	//   - MediaContent with FilePath set (Data should NOT be loaded into memory)
	//   - Error if retrieval fails or reference is invalid
	//
	// The implementation should:
	//   - Validate the reference
	//   - Return MediaContent with FilePath pointing to the stored media
	//   - NOT load the full media data into memory (caller can use GetBase64Data if needed)
	RetrieveMedia(ctx context.Context, reference StorageReference) (*types.MediaContent, error)

	// DeleteMedia deletes media content by its storage reference.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - reference: The storage reference to delete
	//
	// Returns:
	//   - Error if deletion fails or reference is invalid
	//
	// The implementation should:
	//   - Validate the reference
	//   - Delete the media content if not referenced elsewhere (for dedup)
	//   - Clean up any associated metadata
	//   - Handle concurrent deletions safely
	DeleteMedia(ctx context.Context, reference StorageReference) error

	// GetURL returns a URL that can be used to access the media.
	// For local storage, this returns a file:// URL.
	// For cloud storage, this may return a signed URL with expiration.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - reference: The storage reference
	//   - expiry: How long the URL should be valid (ignored for local storage)
	//
	// Returns:
	//   - URL string that can be used to access the media
	//   - Error if URL generation fails or reference is invalid
	GetURL(ctx context.Context, reference StorageReference, expiry time.Duration) (string, error)
}

// PolicyHandler defines the interface for applying and enforcing storage policies.
// Policies control media retention, cleanup, and other lifecycle management.
//
// Example usage:
//
//	policy := policy.NewTimeBasedPolicy()
//	err := policy.ApplyPolicy(ctx, "/path/to/media.jpg", "delete-after-10min")
//	if err != nil {
//	    return err
//	}
//	// Background enforcement
//	go func() {
//	    ticker := time.NewTicker(1 * time.Minute)
//	    for range ticker.C {
//	        policy.EnforcePolicy(ctx)
//	    }
//	}()
type PolicyHandler interface {
	// ApplyPolicy applies a named policy to a media file.
	// This typically stores policy metadata alongside the media.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - filePath: Path to the media file
	//   - policyName: Name of the policy to apply (e.g., "delete-after-10min", "retain-30days")
	//
	// Returns:
	//   - Error if policy application fails or policy is unknown
	ApplyPolicy(ctx context.Context, filePath string, policyName string) error

	// EnforcePolicy scans stored media and enforces policies.
	// This is typically called periodically in the background.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//
	// Returns:
	//   - Error if enforcement fails (should log but not crash on individual file errors)
	//
	// The implementation should:
	//   - Scan media directories for policy metadata
	//   - Apply policies (e.g., delete expired files)
	//   - Log enforcement actions
	//   - Handle errors gracefully (don't stop on permission denied, etc.)
	EnforcePolicy(ctx context.Context) error
}
