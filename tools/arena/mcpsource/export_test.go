package mcpsource

// resetRegistryForTest clears the global registry. Used by unit tests only.
func resetRegistryForTest() {
	regMu.Lock()
	defer regMu.Unlock()
	registered = map[string]MCPSource{}
}
