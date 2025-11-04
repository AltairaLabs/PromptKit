package results

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// CompositeResultRepository writes to multiple repositories simultaneously.
// This allows Arena to output results in multiple formats (JSON + JUnit + HTML)
// with a single call, ensuring consistency across all outputs.
type CompositeResultRepository struct {
	repositories []ResultRepository
}

// NewCompositeRepository creates a new composite repository that writes
// to all provided repositories in order.
func NewCompositeRepository(repos ...ResultRepository) *CompositeResultRepository {
	return &CompositeResultRepository{repositories: repos}
}

// AddRepository adds a new repository to the composite
func (r *CompositeResultRepository) AddRepository(repo ResultRepository) {
	r.repositories = append(r.repositories, repo)
}

// GetRepositories returns all repositories in the composite
func (r *CompositeResultRepository) GetRepositories() []ResultRepository {
	// Return a copy to prevent external modification
	result := make([]ResultRepository, len(r.repositories))
	copy(result, r.repositories)
	return result
}

// SaveResults saves results to all repositories.
// If any repository fails, it continues with others and returns
// a composite error containing all failures.
func (r *CompositeResultRepository) SaveResults(results []engine.RunResult) error {
	var errs []error
	for i, repo := range r.repositories {
		if err := repo.SaveResults(results); err != nil {
			errs = append(errs, fmt.Errorf(repositoryFailedTemplate, i, err))
		}
	}
	if len(errs) > 0 {
		return NewCompositeError("SaveResults", errs)
	}
	return nil
}

// SaveSummary saves summary to all repositories
func (r *CompositeResultRepository) SaveSummary(summary *ResultSummary) error {
	var errs []error
	for i, repo := range r.repositories {
		if err := repo.SaveSummary(summary); err != nil {
			errs = append(errs, fmt.Errorf(repositoryFailedTemplate, i, err))
		}
	}
	if len(errs) > 0 {
		return NewCompositeError("SaveSummary", errs)
	}
	return nil
}

// LoadResults loads from the first repository that supports loading
func (r *CompositeResultRepository) LoadResults() ([]engine.RunResult, error) {
	for i, repo := range r.repositories {
		results, err := repo.LoadResults()
		if err == nil {
			return results, nil
		}
		// Continue to next repository if this one doesn't support loading
		if IsUnsupportedOperation(err) {
			continue
		}
		// Return actual error if it's not just unsupported
		return nil, fmt.Errorf("repository %d load failed: %w", i, err)
	}
	return nil, NewUnsupportedOperationError("LoadResults", "no repositories support loading")
}

// SupportsStreaming returns true if any repository supports streaming
func (r *CompositeResultRepository) SupportsStreaming() bool {
	for _, repo := range r.repositories {
		if repo.SupportsStreaming() {
			return true
		}
	}
	return false
}

// SaveResult saves a single result to all streaming-capable repositories
func (r *CompositeResultRepository) SaveResult(result *engine.RunResult) error {
	var errs []error
	savedAny := false

	for i, repo := range r.repositories {
		if repo.SupportsStreaming() {
			if err := repo.SaveResult(result); err != nil {
				errs = append(errs, fmt.Errorf(repositoryFailedTemplate, i, err))
			} else {
				savedAny = true
			}
		}
	}

	if !savedAny {
		return NewUnsupportedOperationError("SaveResult", "no repositories support streaming")
	}

	if len(errs) > 0 {
		return NewCompositeError("SaveResult", errs)
	}

	return nil
}