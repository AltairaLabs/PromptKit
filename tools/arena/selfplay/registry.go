package selfplay

import (
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/tools/arena/config"
)

// CacheKey represents a structured cache key for user generators
type CacheKey struct {
	Role      string
	PersonaID string
}

// Registry manages self-play providers, personas, and user generator creation
type Registry struct {
	providerRegistry *providers.Registry
	providerMap      map[string]string                  // Maps role ID to provider ID
	personas         map[string]*config.UserPersonaPack // Maps persona ID to persona config
	roles            []config.SelfPlayRoleGroup         // Self-play role configurations
	userGenerators   map[CacheKey]*ContentGenerator     // Cache for created user generators
	cacheHits        int                                // Track cache hits for observability
	cacheMisses      int                                // Track cache misses for observability
	mu               sync.RWMutex                       // Protects concurrent access to cache
}

// NewRegistry creates a new self-play registry
func NewRegistry(
	providerRegistry *providers.Registry,
	providerMap map[string]string,
	personas map[string]*config.UserPersonaPack,
	roles []config.SelfPlayRoleGroup,
) *Registry {
	return &Registry{
		providerRegistry: providerRegistry,
		providerMap:      providerMap,
		personas:         personas,
		roles:            roles,
		userGenerators:   make(map[CacheKey]*ContentGenerator),
		cacheHits:        0,
		cacheMisses:      0,
	}
}

// GetContentGenerator implements Provider interface
// Returns a Generator for the given role and persona (cached for efficiency)
func (r *Registry) GetContentGenerator(role, personaID string) (Generator, error) {
	cacheKey := CacheKey{Role: role, PersonaID: personaID}

	// Check cache first
	r.mu.RLock()
	if cached, exists := r.userGenerators[cacheKey]; exists {
		r.mu.RUnlock()

		// Track cache hit
		r.mu.Lock()
		r.cacheHits++
		r.mu.Unlock()

		return cached, nil
	}
	r.mu.RUnlock()

	// Track cache miss
	r.mu.Lock()
	r.cacheMisses++
	r.mu.Unlock()

	// Create new content generator
	contentGen, err := r.createContentGenerator(role, personaID)
	if err != nil {
		return nil, err
	}

	// Cache the result
	r.mu.Lock()
	r.userGenerators[cacheKey] = contentGen
	r.mu.Unlock()

	return contentGen, nil
}

// IsValidRole checks if a role is configured for self-play
func (r *Registry) IsValidRole(role string) bool {
	for _, roleConfig := range r.roles {
		if roleConfig.ID == role {
			return true
		}
	}
	return false
}

// createContentGenerator creates a new ContentGenerator for the given role and persona
func (r *Registry) createContentGenerator(role, personaID string) (*ContentGenerator, error) {
	// Validate persona
	if personaID == "" {
		return nil, fmt.Errorf("persona ID is required for self-play turns")
	}

	persona, exists := r.personas[personaID]
	if !exists {
		return nil, fmt.Errorf("persona not found: %s", personaID)
	}

	// Find and validate role configuration
	var foundRole bool
	for _, roleConfig := range r.roles {
		if roleConfig.ID == role {
			foundRole = true
			break
		}
	}

	if !foundRole {
		return nil, fmt.Errorf("self-play role configuration not found: %s", role)
	}

	// Get provider for this role
	provider, err := r.getProviderForRole(role)
	if err != nil {
		return nil, err
	}

	// Create and return user generator
	userGenerator := NewContentGenerator(provider, persona)
	return userGenerator, nil
}

// getProviderForRole resolves the provider for a given role
func (r *Registry) getProviderForRole(role string) (providers.Provider, error) {
	// Check if this role has a provider mapping
	providerID, exists := r.providerMap[role]
	if !exists {
		logger.Debug("No provider configured for self-play role", "component", "selfplay", "role", role)
		return nil, fmt.Errorf("no provider configured for self-play role: %s", role)
	}

	logger.Debug("Using self-play provider mapping", "component", "selfplay", "role", role, "provider", providerID)

	// Get provider from registry
	provider, exists := r.providerRegistry.Get(providerID)
	if !exists {
		logger.Debug("Self-play provider not found in registry", "component", "selfplay", "provider", providerID)
		availableProviders := r.providerRegistry.List()
		return nil, fmt.Errorf("provider not found for role %s: %s (available providers: %v)", role, providerID, availableProviders)
	}

	logger.Debug("Found self-play provider", "component", "selfplay", "provider", providerID)
	return provider, nil
}

// GetAvailableRoles returns a list of available self-play roles
func (r *Registry) GetAvailableRoles() []string {
	roles := make([]string, 0, len(r.roles))
	for _, role := range r.roles {
		roles = append(roles, role.ID)
	}
	return roles
}

// GetAvailableProviders returns a list of available providers
func (r *Registry) GetAvailableProviders() []string {
	return r.providerRegistry.List()
}

// PrewarmCache creates and caches ContentGenerators for common role+persona combinations
// This can improve performance by avoiding cold starts during actual execution
func (r *Registry) PrewarmCache(rolePairs []CacheKey) error {
	for _, pair := range rolePairs {
		// Check if already cached
		r.mu.RLock()
		_, exists := r.userGenerators[pair]
		r.mu.RUnlock()

		if exists {
			continue // Already cached
		}

		// Create and cache the user generator
		_, err := r.GetContentGenerator(pair.Role, pair.PersonaID)
		if err != nil {
			return fmt.Errorf("failed to prewarm cache for %s:%s: %w", pair.Role, pair.PersonaID, err)
		}
	}
	return nil
}

// CacheStats provides observability into cache performance
type CacheStats struct {
	Size        int     // Number of cached entries
	Hits        int     // Number of cache hits
	Misses      int     // Number of cache misses
	HitRate     float64 // Cache hit rate (0.0 to 1.0)
	CachedPairs []CacheKey
}

// GetCacheStats returns current cache statistics
func (r *Registry) GetCacheStats() CacheStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	totalRequests := r.cacheHits + r.cacheMisses
	hitRate := 0.0
	if totalRequests > 0 {
		hitRate = float64(r.cacheHits) / float64(totalRequests)
	}

	// Get list of cached pairs
	cachedPairs := make([]CacheKey, 0, len(r.userGenerators))
	for key := range r.userGenerators {
		cachedPairs = append(cachedPairs, key)
	}

	return CacheStats{
		Size:        len(r.userGenerators),
		Hits:        r.cacheHits,
		Misses:      r.cacheMisses,
		HitRate:     hitRate,
		CachedPairs: cachedPairs,
	}
}

// ClearCache clears all cached user generators and resets statistics
func (r *Registry) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.userGenerators = make(map[CacheKey]*ContentGenerator)
	r.cacheHits = 0
	r.cacheMisses = 0
}

// Close closes the self-play provider registry and cleans up resources
func (r *Registry) Close() error {
	if r.providerRegistry != nil {
		return r.providerRegistry.Close()
	}
	return nil
}
