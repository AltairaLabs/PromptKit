package annotations

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store persists annotations separately from the event stream.
type Store interface {
	// Add creates a new annotation.
	Add(ctx context.Context, ann *Annotation) error

	// Update creates a new version of an existing annotation.
	// The new annotation will reference the previous version.
	Update(ctx context.Context, previousID string, ann *Annotation) error

	// Get retrieves an annotation by ID.
	Get(ctx context.Context, id string) (*Annotation, error)

	// Query returns annotations matching the filter.
	Query(ctx context.Context, filter *Filter) ([]*Annotation, error)

	// Delete removes an annotation by ID.
	// Note: This is a soft delete - the annotation is marked as deleted but preserved.
	Delete(ctx context.Context, id string) error

	// Close releases resources held by the store.
	Close() error
}

// Filter specifies criteria for querying annotations.
type Filter struct {
	// SessionID filters by session.
	SessionID string

	// Types filters by annotation type.
	Types []AnnotationType

	// Keys filters by annotation key.
	Keys []string

	// TargetTypes filters by target type.
	TargetTypes []TargetType

	// EventID filters by target event ID.
	EventID string

	// TurnIndex filters by target turn index.
	TurnIndex *int

	// CreatedBy filters by creator.
	CreatedBy string

	// Since filters by creation time (inclusive).
	Since time.Time

	// Until filters by creation time (exclusive).
	Until time.Time

	// IncludeDeleted includes deleted annotations.
	IncludeDeleted bool

	// LatestVersionOnly returns only the latest version of each annotation.
	LatestVersionOnly bool

	// Limit limits the number of results.
	Limit int
}

// FileStore implements Store using JSON Lines files.
// Annotations for each session are stored in a separate file.
type FileStore struct {
	dir   string
	mu    sync.RWMutex
	files map[string]*os.File
}

// Store constants.
const (
	storeDirPermissions  = 0750
	storeFilePermissions = 0600
	scannerBufferSize    = 1024 * 1024 // 1MB buffer
)

// NewFileStore creates a file-based annotation store.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, storeDirPermissions); err != nil {
		return nil, fmt.Errorf("create annotation store directory: %w", err)
	}
	return &FileStore{
		dir:   dir,
		files: make(map[string]*os.File),
	}, nil
}

// storedAnnotation wraps an Annotation with storage metadata.
type storedAnnotation struct {
	Annotation *Annotation `json:"annotation"`
	Deleted    bool        `json:"deleted,omitempty"`
	DeletedAt  *time.Time  `json:"deleted_at,omitempty"`
}

// Add creates a new annotation.
func (s *FileStore) Add(ctx context.Context, ann *Annotation) error {
	if ann.SessionID == "" {
		return fmt.Errorf("annotation has no session ID")
	}

	// Generate ID if not set
	if ann.ID == "" {
		ann.ID = uuid.New().String()
	}

	// Set creation time if not set
	if ann.CreatedAt.IsZero() {
		ann.CreatedAt = time.Now().UTC()
	}

	// Set version to 1 for new annotations
	if ann.Version == 0 {
		ann.Version = 1
	}

	return s.write(ctx, ann.SessionID, &storedAnnotation{Annotation: ann})
}

// Update creates a new version of an existing annotation.
func (s *FileStore) Update(ctx context.Context, previousID string, ann *Annotation) error {
	if ann.SessionID == "" {
		return fmt.Errorf("annotation has no session ID")
	}

	// Get the previous annotation to increment version
	prev, err := s.Get(ctx, previousID)
	if err != nil {
		return fmt.Errorf("get previous annotation: %w", err)
	}

	// Generate new ID
	ann.ID = uuid.New().String()
	ann.PreviousID = previousID
	ann.Version = prev.Version + 1
	ann.CreatedAt = time.Now().UTC()

	return s.write(ctx, ann.SessionID, &storedAnnotation{Annotation: ann})
}

// Get retrieves an annotation by ID.
func (s *FileStore) Get(ctx context.Context, id string) (*Annotation, error) {
	// We need to search all session files since we don't know the session ID
	// A more efficient implementation would maintain an index
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("annotation not found: %s", id)
		}
		return nil, fmt.Errorf("read directory: %w", err)
	}

	for _, entry := range files {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}

		path := filepath.Join(s.dir, entry.Name())
		ann, err := s.findByID(ctx, path, id)
		if err == nil && ann != nil {
			return ann, nil
		}
	}

	return nil, fmt.Errorf("annotation not found: %s", id)
}

// findByID searches a file for an annotation by ID.
func (s *FileStore) findByID(ctx context.Context, path, id string) (*Annotation, error) {
	f, err := os.Open(path) //nolint:gosec // path is constructed internally
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufferSize), scannerBufferSize)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var stored storedAnnotation
		if json.Unmarshal(scanner.Bytes(), &stored) != nil {
			continue
		}

		if stored.Annotation != nil && stored.Annotation.ID == id {
			if stored.Deleted {
				return nil, fmt.Errorf("annotation deleted: %s", id)
			}
			return stored.Annotation, nil
		}
	}

	return nil, nil
}

// Query returns annotations matching the filter.
func (s *FileStore) Query(ctx context.Context, filter *Filter) ([]*Annotation, error) {
	if filter.SessionID == "" {
		return nil, fmt.Errorf("session ID required for query")
	}

	deletedIDs, annotationsByID, err := s.loadAnnotations(ctx, filter.SessionID)
	if err != nil {
		return nil, err
	}

	results := s.filterAnnotations(annotationsByID, deletedIDs, filter)
	return s.applyLimit(results, filter.Limit), nil
}

// loadAnnotations reads all annotations for a session from disk.
func (s *FileStore) loadAnnotations(
	ctx context.Context, sessionID string,
) (deletedIDs map[string]bool, annotationsByID map[string]*Annotation, err error) {
	path := s.sessionPath(sessionID)
	f, err := os.Open(path) //nolint:gosec // path constructed from trusted sessionID
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("open annotations file: %w", err)
	}
	defer f.Close()

	deletedIDs = make(map[string]bool)
	annotationsByID = make(map[string]*Annotation)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufferSize), scannerBufferSize)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		var stored storedAnnotation
		if json.Unmarshal(scanner.Bytes(), &stored) != nil {
			continue
		}

		if stored.Annotation == nil {
			continue
		}

		if stored.Deleted {
			deletedIDs[stored.Annotation.ID] = true
		}
		annotationsByID[stored.Annotation.ID] = stored.Annotation
	}

	return deletedIDs, annotationsByID, scanner.Err()
}

// filterAnnotations applies filter criteria to the annotation map.
func (s *FileStore) filterAnnotations(
	annotationsByID map[string]*Annotation,
	deletedIDs map[string]bool,
	filter *Filter,
) []*Annotation {
	latestVersions := make(map[string]*Annotation)
	var results []*Annotation

	for id, ann := range annotationsByID {
		if !s.shouldInclude(id, ann, deletedIDs, filter) {
			continue
		}

		if filter.LatestVersionOnly {
			s.trackLatestVersion(latestVersions, ann)
		} else {
			results = append(results, ann)
		}
	}

	if filter.LatestVersionOnly {
		for _, ann := range latestVersions {
			results = append(results, ann)
		}
	}

	return results
}

// shouldInclude checks if an annotation should be included in results.
func (s *FileStore) shouldInclude(
	id string, ann *Annotation, deletedIDs map[string]bool, filter *Filter,
) bool {
	if deletedIDs[id] && !filter.IncludeDeleted {
		return false
	}
	return s.matchesFilter(ann, filter)
}

// trackLatestVersion tracks the latest version of each annotation by key.
func (s *FileStore) trackLatestVersion(latestVersions map[string]*Annotation, ann *Annotation) {
	existing, ok := latestVersions[ann.Key]
	if !ok || ann.Version > existing.Version {
		latestVersions[ann.Key] = ann
	}
}

// applyLimit truncates results to the specified limit.
func (s *FileStore) applyLimit(results []*Annotation, limit int) []*Annotation {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

// Delete marks an annotation as deleted.
func (s *FileStore) Delete(ctx context.Context, id string) error {
	// Get the annotation first to find its session
	ann, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	return s.write(ctx, ann.SessionID, &storedAnnotation{
		Annotation: ann,
		Deleted:    true,
		DeletedAt:  &now,
	})
}

// Close releases resources.
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	for _, f := range s.files {
		if err := f.Sync(); err != nil {
			errs = append(errs, err)
		}
		if err := f.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	s.files = make(map[string]*os.File)

	if len(errs) > 0 {
		return fmt.Errorf("close files: %v", errs)
	}
	return nil
}

// write appends a stored annotation to the session file.
func (s *FileStore) write(ctx context.Context, sessionID string, stored *storedAnnotation) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	data, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("marshal annotation: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.getOrCreateFile(sessionID)
	if err != nil {
		return err
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write annotation: %w", err)
	}

	return nil
}

// sessionPath returns the file path for a session's annotations.
func (s *FileStore) sessionPath(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".annotations.jsonl")
}

// getOrCreateFile returns the file for a session, creating it if needed.
// Caller must hold s.mu.
func (s *FileStore) getOrCreateFile(sessionID string) (*os.File, error) {
	if f, ok := s.files[sessionID]; ok {
		return f, nil
	}

	path := s.sessionPath(sessionID)
	//nolint:gosec // path is constructed from trusted sessionID
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, storeFilePermissions)
	if err != nil {
		return nil, fmt.Errorf("create annotations file: %w", err)
	}

	s.files[sessionID] = f
	return f, nil
}

// matchesFilter checks if an annotation matches the filter criteria.
func (s *FileStore) matchesFilter(ann *Annotation, filter *Filter) bool {
	if !s.matchesTypes(ann.Type, filter.Types) {
		return false
	}
	if !s.matchesKeys(ann.Key, filter.Keys) {
		return false
	}
	if !s.matchesTargetTypes(ann.Target.Type, filter.TargetTypes) {
		return false
	}
	if filter.EventID != "" && ann.Target.EventID != filter.EventID {
		return false
	}
	if filter.TurnIndex != nil && ann.Target.TurnIndex != *filter.TurnIndex {
		return false
	}
	if filter.CreatedBy != "" && ann.CreatedBy != filter.CreatedBy {
		return false
	}
	if !filter.Since.IsZero() && ann.CreatedAt.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && ann.CreatedAt.After(filter.Until) {
		return false
	}
	return true
}

// matchesTypes checks if the type is in the allowed list.
func (s *FileStore) matchesTypes(t AnnotationType, types []AnnotationType) bool {
	if len(types) == 0 {
		return true
	}
	for _, allowed := range types {
		if t == allowed {
			return true
		}
	}
	return false
}

// matchesKeys checks if the key is in the allowed list.
func (s *FileStore) matchesKeys(key string, keys []string) bool {
	if len(keys) == 0 {
		return true
	}
	for _, k := range keys {
		if key == k {
			return true
		}
	}
	return false
}

// matchesTargetTypes checks if the target type is in the allowed list.
func (s *FileStore) matchesTargetTypes(t TargetType, types []TargetType) bool {
	if len(types) == 0 {
		return true
	}
	for _, allowed := range types {
		if t == allowed {
			return true
		}
	}
	return false
}

// Ensure FileStore implements Store.
var _ Store = (*FileStore)(nil)
