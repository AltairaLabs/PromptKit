package selfplay

import (
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/tools/arena/config"
)

// Test helper: creates a basic mock provider
func createMockProvider(id string) providers.Provider {
	return providers.NewOpenAIProvider(id, "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{
		Temperature: 0.7,
		MaxTokens:   1000,
	}, false)
}

// Test helper: creates a basic persona pack
func createTestPersona(id, prompt string) *config.UserPersonaPack {
	return &config.UserPersonaPack{
		ID:           id,
		SystemPrompt: prompt,
		Style: config.PersonaStyle{
			Verbosity:      "normal",
			ChallengeLevel: "medium",
		},
	}
}

func TestNewRegistry(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	providerMap := map[string]string{"user": "provider1"}
	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "You are helpful"),
	}
	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	if registry == nil {
		t.Fatal("Expected registry to be created")
	}

	if registry.providerRegistry != providerRegistry {
		t.Error("Expected provider registry to be set")
	}

	if len(registry.providerMap) != 1 {
		t.Error("Expected provider map to be set")
	}

	if len(registry.personas) != 1 {
		t.Error("Expected personas to be set")
	}

	if len(registry.roles) != 1 {
		t.Error("Expected roles to be set")
	}

	if registry.userGenerators == nil {
		t.Error("Expected user generators map to be initialized")
	}

	if registry.cacheHits != 0 || registry.cacheMisses != 0 {
		t.Error("Expected cache stats to be initialized to 0")
	}
}

func TestSelfPlayRegistry_IsValidRole(t *testing.T) {
	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
		{ID: "assistant"},
	}

	registry := NewRegistry(
		providers.NewRegistry(),
		map[string]string{},
		map[string]*config.UserPersonaPack{},
		roles,
	)

	tests := []struct {
		name     string
		role     string
		expected bool
	}{
		{"Valid role - user", "user", true},
		{"Valid role - assistant", "assistant", true},
		{"Invalid role", "admin", false},
		{"Empty role", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registry.IsValidRole(tt.role)
			if result != tt.expected {
				t.Errorf("IsValidRole(%s) = %v, want %v", tt.role, result, tt.expected)
			}
		})
	}
}

func TestSelfPlayRegistry_GetAvailableRoles(t *testing.T) {
	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
		{ID: "assistant"},
		{ID: "reviewer"},
	}

	registry := NewRegistry(
		providers.NewRegistry(),
		map[string]string{},
		map[string]*config.UserPersonaPack{},
		roles,
	)

	availableRoles := registry.GetAvailableRoles()

	if len(availableRoles) != 3 {
		t.Errorf("Expected 3 roles, got %d", len(availableRoles))
	}

	roleMap := make(map[string]bool)
	for _, role := range availableRoles {
		roleMap[role] = true
	}

	if !roleMap["user"] || !roleMap["assistant"] || !roleMap["reviewer"] {
		t.Errorf("Expected roles [user, assistant, reviewer], got %v", availableRoles)
	}
}

func TestSelfPlayRegistry_GetAvailableRoles_Empty(t *testing.T) {
	registry := NewRegistry(
		providers.NewRegistry(),
		map[string]string{},
		map[string]*config.UserPersonaPack{},
		[]config.SelfPlayRoleGroup{},
	)

	availableRoles := registry.GetAvailableRoles()

	if len(availableRoles) != 0 {
		t.Errorf("Expected 0 roles, got %d", len(availableRoles))
	}
}

func TestSelfPlayRegistry_GetAvailableProviders(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider1 := createMockProvider("provider1")
	provider2 := createMockProvider("provider2")
	providerRegistry.Register(provider1)
	providerRegistry.Register(provider2)

	registry := NewRegistry(
		providerRegistry,
		map[string]string{},
		map[string]*config.UserPersonaPack{},
		[]config.SelfPlayRoleGroup{},
	)

	availableProviders := registry.GetAvailableProviders()

	if len(availableProviders) != 2 {
		t.Errorf("Expected 2 providers, got %d", len(availableProviders))
	}
}

func TestSelfPlayRegistry_GetContentGenerator_Success(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "You are helpful"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "provider1",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	userGen, err := registry.GetContentGenerator("user", "persona1")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if userGen == nil {
		t.Fatal("Expected content generator to be created")
	}

	// Type assert to access concrete fields for testing
	concreteGen, ok := userGen.(*ContentGenerator)
	if !ok {
		t.Fatal("Expected userGen to be *ContentGenerator")
	}

	if concreteGen.provider != provider {
		t.Error("Expected content generator to have correct provider")
	}

	if concreteGen.persona != personas["persona1"] {
		t.Error("Expected content generator to have correct persona")
	}

	// Verify cache stats
	stats := registry.GetCacheStats()
	if stats.Misses != 1 {
		t.Errorf("Expected 1 cache miss, got %d", stats.Misses)
	}
	if stats.Hits != 0 {
		t.Errorf("Expected 0 cache hits, got %d", stats.Hits)
	}
	if stats.Size != 1 {
		t.Errorf("Expected cache size 1, got %d", stats.Size)
	}
}

func TestSelfPlayRegistry_GetContentGenerator_CacheHit(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "You are helpful"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "provider1",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	// First call - cache miss
	userGen1, err := registry.GetContentGenerator("user", "persona1")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Second call - cache hit
	userGen2, err := registry.GetContentGenerator("user", "persona1")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if userGen1 != userGen2 {
		t.Error("Expected same user generator instance from cache")
	}

	// Verify cache stats
	stats := registry.GetCacheStats()
	if stats.Misses != 1 {
		t.Errorf("Expected 1 cache miss, got %d", stats.Misses)
	}
	if stats.Hits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", stats.Hits)
	}
	if stats.Size != 1 {
		t.Errorf("Expected cache size 1, got %d", stats.Size)
	}
	if stats.HitRate != 0.5 {
		t.Errorf("Expected hit rate 0.5, got %f", stats.HitRate)
	}
}

func TestSelfPlayRegistry_GetContentGenerator_EmptyPersonaID(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "provider1",
	}

	registry := NewRegistry(
		providerRegistry,
		providerMap,
		map[string]*config.UserPersonaPack{},
		roles,
	)

	_, err := registry.GetContentGenerator("user", "")
	if err == nil {
		t.Fatal("Expected error for empty persona ID")
	}

	if err.Error() != "persona ID is required for self-play turns" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestSelfPlayRegistry_GetContentGenerator_PersonaNotFound(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "provider1",
	}

	registry := NewRegistry(
		providerRegistry,
		providerMap,
		map[string]*config.UserPersonaPack{},
		roles,
	)

	_, err := registry.GetContentGenerator("user", "nonexistent")
	if err == nil {
		t.Fatal("Expected error for nonexistent persona")
	}

	expectedErr := "persona not found: nonexistent"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %q, got %q", expectedErr, err.Error())
	}
}

func TestSelfPlayRegistry_GetContentGenerator_RoleNotFound(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "You are helpful"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "provider1",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	_, err := registry.GetContentGenerator("admin", "persona1")
	if err == nil {
		t.Fatal("Expected error for nonexistent role")
	}

	expectedErr := "self-play role configuration not found: admin"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %q, got %q", expectedErr, err.Error())
	}
}

func TestSelfPlayRegistry_GetContentGenerator_NoProviderMapping(t *testing.T) {
	providerRegistry := providers.NewRegistry()

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "You are helpful"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	// No provider map for "user" role
	providerMap := map[string]string{}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	_, err := registry.GetContentGenerator("user", "persona1")
	if err == nil {
		t.Fatal("Expected error for missing provider mapping")
	}

	expectedErr := "no provider configured for self-play role: user"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %q, got %q", expectedErr, err.Error())
	}
}

func TestSelfPlayRegistry_GetContentGenerator_ProviderNotInRegistry(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	// Provider not registered

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "You are helpful"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "nonexistent-provider",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	_, err := registry.GetContentGenerator("user", "persona1")
	if err == nil {
		t.Fatal("Expected error for provider not in registry")
	}

	// Error message includes available providers list
	if err.Error() == "" {
		t.Error("Expected error message")
	}
}

func TestSelfPlayRegistry_PrewarmCache_Success(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "Persona 1"),
		"persona2": createTestPersona("persona2", "Persona 2"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
		{ID: "assistant"},
	}

	providerMap := map[string]string{
		"user":      "provider1",
		"assistant": "provider1",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	// Prewarm cache with specific combinations
	pairs := []CacheKey{
		{Role: "user", PersonaID: "persona1"},
		{Role: "user", PersonaID: "persona2"},
		{Role: "assistant", PersonaID: "persona1"},
	}

	err := registry.PrewarmCache(pairs)
	if err != nil {
		t.Fatalf("Expected no error from PrewarmCache, got: %v", err)
	}

	// Verify all pairs are cached
	stats := registry.GetCacheStats()
	if stats.Size != 3 {
		t.Errorf("Expected cache size 3, got %d", stats.Size)
	}

	// All requests should have been cache misses during prewarm
	if stats.Misses != 3 {
		t.Errorf("Expected 3 cache misses, got %d", stats.Misses)
	}

	// Now accessing should be cache hits
	_, err = registry.GetContentGenerator("user", "persona1")
	if err != nil {
		t.Fatal("Expected cached generator to be accessible")
	}

	stats = registry.GetCacheStats()
	if stats.Hits != 1 {
		t.Errorf("Expected 1 cache hit after prewarm, got %d", stats.Hits)
	}
}

func TestSelfPlayRegistry_PrewarmCache_AlreadyCached(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "Persona 1"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "provider1",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	// First cache it manually
	_, err := registry.GetContentGenerator("user", "persona1")
	if err != nil {
		t.Fatal(err)
	}

	// Prewarm with same pair - should skip
	pairs := []CacheKey{
		{Role: "user", PersonaID: "persona1"},
	}

	err = registry.PrewarmCache(pairs)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	stats := registry.GetCacheStats()
	// Should still be 1 miss (from manual call), no additional misses
	if stats.Misses != 1 {
		t.Errorf("Expected 1 cache miss, got %d", stats.Misses)
	}
}

func TestSelfPlayRegistry_PrewarmCache_Error(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "Persona 1"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "provider1",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	// Try to prewarm with invalid persona
	pairs := []CacheKey{
		{Role: "user", PersonaID: "nonexistent"},
	}

	err := registry.PrewarmCache(pairs)
	if err == nil {
		t.Fatal("Expected error from PrewarmCache with invalid persona")
	}

	// Error should mention the invalid combination
	if err.Error() == "" {
		t.Error("Expected error message")
	}
}

func TestSelfPlayRegistry_GetCacheStats_EmptyCache(t *testing.T) {
	registry := NewRegistry(
		providers.NewRegistry(),
		map[string]string{},
		map[string]*config.UserPersonaPack{},
		[]config.SelfPlayRoleGroup{},
	)

	stats := registry.GetCacheStats()

	if stats.Size != 0 {
		t.Errorf("Expected size 0, got %d", stats.Size)
	}
	if stats.Hits != 0 {
		t.Errorf("Expected hits 0, got %d", stats.Hits)
	}
	if stats.Misses != 0 {
		t.Errorf("Expected misses 0, got %d", stats.Misses)
	}
	if stats.HitRate != 0.0 {
		t.Errorf("Expected hit rate 0.0, got %f", stats.HitRate)
	}
	if len(stats.CachedPairs) != 0 {
		t.Errorf("Expected 0 cached pairs, got %d", len(stats.CachedPairs))
	}
}

func TestSelfPlayRegistry_GetCacheStats_WithData(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "Persona 1"),
		"persona2": createTestPersona("persona2", "Persona 2"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "provider1",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	// Create cache misses
	_, _ = registry.GetContentGenerator("user", "persona1")
	_, _ = registry.GetContentGenerator("user", "persona2")

	// Create cache hits
	_, _ = registry.GetContentGenerator("user", "persona1")
	_, _ = registry.GetContentGenerator("user", "persona1")
	_, _ = registry.GetContentGenerator("user", "persona2")

	stats := registry.GetCacheStats()
	if stats.Size != 2 {
		t.Errorf("Expected size 2, got %d", stats.Size)
	}
	if stats.Hits != 3 {
		t.Errorf("Expected hits 3, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("Expected misses 2, got %d", stats.Misses)
	}

	expectedHitRate := 3.0 / 5.0 // 3 hits out of 5 total requests
	if stats.HitRate != expectedHitRate {
		t.Errorf("Expected hit rate %f, got %f", expectedHitRate, stats.HitRate)
	}

	if len(stats.CachedPairs) != 2 {
		t.Errorf("Expected 2 cached pairs, got %d", len(stats.CachedPairs))
	}
}

func TestSelfPlayRegistry_ClearCache(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "Persona 1"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "provider1",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	// Populate cache
	_, _ = registry.GetContentGenerator("user", "persona1")
	_, _ = registry.GetContentGenerator("user", "persona1") // Create a cache hit

	// Verify cache has data
	stats := registry.GetCacheStats()
	if stats.Size != 1 || stats.Hits != 1 || stats.Misses != 1 {
		t.Fatal("Cache should have data before clearing")
	}

	// Clear cache
	registry.ClearCache()

	// Verify cache is empty
	stats = registry.GetCacheStats()
	if stats.Size != 0 {
		t.Errorf("Expected size 0 after clear, got %d", stats.Size)
	}
	if stats.Hits != 0 {
		t.Errorf("Expected hits 0 after clear, got %d", stats.Hits)
	}
	if stats.Misses != 0 {
		t.Errorf("Expected misses 0 after clear, got %d", stats.Misses)
	}
	if stats.HitRate != 0.0 {
		t.Errorf("Expected hit rate 0.0 after clear, got %f", stats.HitRate)
	}

	// Access should create new cache miss
	_, err := registry.GetContentGenerator("user", "persona1")
	if err != nil {
		t.Fatal(err)
	}

	stats = registry.GetCacheStats()
	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss after clear and new access, got %d", stats.Misses)
	}
}

func TestSelfPlayRegistry_ConcurrentAccess(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "Persona 1"),
		"persona2": createTestPersona("persona2", "Persona 2"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
	}

	providerMap := map[string]string{
		"user": "provider1",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	// Run multiple goroutines accessing the cache concurrently
	const numGoroutines = 100
	const numIterations = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			// Each goroutine accesses different combinations
			personaID := "persona1"
			if id%2 == 0 {
				personaID = "persona2"
			}

			for j := 0; j < numIterations; j++ {
				_, err := registry.GetContentGenerator("user", personaID)
				if err != nil {
					t.Errorf("Goroutine %d iteration %d: %v", id, j, err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify cache is in consistent state
	stats := registry.GetCacheStats()

	// Should have cached 2 combinations (persona1 and persona2)
	if stats.Size != 2 {
		t.Errorf("Expected cache size 2, got %d", stats.Size)
	}

	// Total requests should be numGoroutines * numIterations
	totalRequests := stats.Hits + stats.Misses
	expectedTotal := numGoroutines * numIterations
	if totalRequests != expectedTotal {
		t.Errorf("Expected %d total requests, got %d", expectedTotal, totalRequests)
	}

	// Due to concurrency, we might have slightly more than 2 misses
	// (multiple goroutines can race to create the same cache entry)
	// But it should be much less than total requests
	if stats.Misses < 2 {
		t.Errorf("Expected at least 2 cache misses, got %d", stats.Misses)
	}

	if stats.Misses > 100 { // Allow up to 10% misses due to race conditions
		t.Errorf("Expected fewer than 100 cache misses with concurrency, got %d", stats.Misses)
	}

	// Most should be hits
	if stats.Hits < 900 { // At least 90% hit rate
		t.Errorf("Expected at least 900 cache hits, got %d", stats.Hits)
	}
}

func TestSelfPlayRegistry_MultipleRolesSamePersona(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider1 := createMockProvider("provider1")
	provider2 := createMockProvider("provider2")
	providerRegistry.Register(provider1)
	providerRegistry.Register(provider2)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "Versatile persona"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
		{ID: "assistant"},
	}

	providerMap := map[string]string{
		"user":      "provider1",
		"assistant": "provider2",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	// Get user generator for user role
	userGen, err := registry.GetContentGenerator("user", "persona1")
	if err != nil {
		t.Fatal(err)
	}

	// Get user generator for assistant role (same persona, different role)
	assistantGen, err := registry.GetContentGenerator("assistant", "persona1")
	if err != nil {
		t.Fatal(err)
	}

	// Should be different instances (different cache keys)
	if userGen == assistantGen {
		t.Error("Expected different content generators for different roles")
	}

	// Type assert to access concrete fields
	userConcrete, ok1 := userGen.(*ContentGenerator)
	assistantConcrete, ok2 := assistantGen.(*ContentGenerator)
	if !ok1 || !ok2 {
		t.Fatal("Expected generators to be *ContentGenerator")
	}

	// But should have same persona
	if userConcrete.persona != assistantConcrete.persona {
		t.Error("Expected same persona instance")
	}

	// Should have different providers
	if userConcrete.provider == assistantConcrete.provider {
		t.Error("Expected different providers for different roles")
	}

	// Cache should have 2 entries
	stats := registry.GetCacheStats()
	if stats.Size != 2 {
		t.Errorf("Expected cache size 2, got %d", stats.Size)
	}
}

func TestCacheKey_Equality(t *testing.T) {
	key1 := CacheKey{Role: "user", PersonaID: "persona1"}
	key2 := CacheKey{Role: "user", PersonaID: "persona1"}
	key3 := CacheKey{Role: "assistant", PersonaID: "persona1"}
	key4 := CacheKey{Role: "user", PersonaID: "persona2"}

	// Test that map lookup works correctly
	m := make(map[CacheKey]string)
	m[key1] = "value1"

	if m[key2] != "value1" {
		t.Error("Expected key1 and key2 to be equal")
	}

	if m[key3] != "" {
		t.Error("Expected key3 to be different")
	}

	if m[key4] != "" {
		t.Error("Expected key4 to be different")
	}
}

func TestSelfPlayRegistry_GetCacheStats_CachedPairs(t *testing.T) {
	providerRegistry := providers.NewRegistry()
	provider := createMockProvider("provider1")
	providerRegistry.Register(provider)

	personas := map[string]*config.UserPersonaPack{
		"persona1": createTestPersona("persona1", "Persona 1"),
		"persona2": createTestPersona("persona2", "Persona 2"),
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "user"},
		{ID: "assistant"},
	}

	providerMap := map[string]string{
		"user":      "provider1",
		"assistant": "provider1",
	}

	registry := NewRegistry(providerRegistry, providerMap, personas, roles)

	// Cache specific combinations
	_, _ = registry.GetContentGenerator("user", "persona1")
	_, _ = registry.GetContentGenerator("assistant", "persona1")

	stats := registry.GetCacheStats()

	// Verify we get the cached pairs
	if len(stats.CachedPairs) != 2 {
		t.Fatalf("Expected 2 cached pairs, got %d", len(stats.CachedPairs))
	}

	// Create a map for easier checking
	pairMap := make(map[CacheKey]bool)
	for _, pair := range stats.CachedPairs {
		pairMap[pair] = true
	}

	expected1 := CacheKey{Role: "user", PersonaID: "persona1"}
	expected2 := CacheKey{Role: "assistant", PersonaID: "persona1"}

	if !pairMap[expected1] {
		t.Error("Expected to find user:persona1 in cached pairs")
	}

	if !pairMap[expected2] {
		t.Error("Expected to find assistant:persona1 in cached pairs")
	}
}
