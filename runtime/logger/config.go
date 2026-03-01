package logger

import (
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// ModuleConfig manages per-module logging configuration.
// It supports hierarchical module names where more specific modules
// override less specific ones (e.g., "runtime.pipeline" overrides "runtime").
type ModuleConfig struct {
	defaultLevel slog.Level
	modules      map[string]slog.Level
	sortedKeys   []string // sorted by specificity (most specific first)
	mu           sync.RWMutex
}

// NewModuleConfig creates a new ModuleConfig with the given default level.
func NewModuleConfig(defaultLevel slog.Level) *ModuleConfig {
	return &ModuleConfig{
		defaultLevel: defaultLevel,
		modules:      make(map[string]slog.Level),
	}
}

// SetModuleLevel sets the log level for a specific module.
// Module names use dot notation (e.g., "runtime.pipeline").
func (m *ModuleConfig) SetModuleLevel(module string, level slog.Level) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.modules[module] = level
	m.updateSortedKeys()
}

// SetDefaultLevel sets the default log level.
func (m *ModuleConfig) SetDefaultLevel(level slog.Level) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultLevel = level
}

// LevelFor returns the log level for the given module.
// It checks for exact match first, then walks up the hierarchy.
// For example, for "runtime.pipeline.stage":
//  1. Check "runtime.pipeline.stage" (exact match)
//  2. Check "runtime.pipeline" (parent)
//  3. Check "runtime" (grandparent)
//  4. Return default level
func (m *ModuleConfig) LevelFor(module string) slog.Level {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check exact match first
	if level, ok := m.modules[module]; ok {
		return level
	}

	// Walk up the hierarchy
	for {
		lastDot := strings.LastIndex(module, ".")
		if lastDot == -1 {
			break
		}
		module = module[:lastDot]
		if level, ok := m.modules[module]; ok {
			return level
		}
	}

	return m.defaultLevel
}

// updateSortedKeys updates the sorted keys list.
// Keys are sorted by specificity (number of dots) in descending order.
// Must be called with lock held.
func (m *ModuleConfig) updateSortedKeys() {
	m.sortedKeys = make([]string, 0, len(m.modules))
	for k := range m.modules {
		m.sortedKeys = append(m.sortedKeys, k)
	}
	// Sort by number of dots (more specific first)
	sort.Slice(m.sortedKeys, func(i, j int) bool {
		dotsI := strings.Count(m.sortedKeys[i], ".")
		dotsJ := strings.Count(m.sortedKeys[j], ".")
		if dotsI != dotsJ {
			return dotsI > dotsJ
		}
		return m.sortedKeys[i] < m.sortedKeys[j]
	})
}

// globalModuleConfig is the global module configuration.
var globalModuleConfig = NewModuleConfig(slog.LevelInfo)

// LoggingConfigSpec defines the logging configuration for the Configure function.
// This mirrors the config.LoggingConfigSpec to avoid import cycles.
type LoggingConfigSpec struct {
	DefaultLevel string
	Format       string // "json" or "text"
	CommonFields map[string]string
	Modules      []ModuleLoggingSpec
}

// ModuleLoggingSpec configures logging for a specific module.
type ModuleLoggingSpec struct {
	Name   string
	Level  string
	Fields map[string]string
}

// Log format constants
const (
	FormatJSON = "json"
	FormatText = "text"
)

// Configure applies a LoggingConfigSpec to the global logger.
// This reconfigures the logger with the new settings.
func Configure(cfg *LoggingConfigSpec) error {
	if cfg == nil {
		return nil
	}

	// If a custom logger was set via SetLogger(), preserve it.
	if customHandler != nil {
		return nil
	}

	// Parse and set default level
	defaultLevel := slog.LevelInfo
	if cfg.DefaultLevel != "" {
		defaultLevel = ParseLevel(cfg.DefaultLevel)
	}

	// Build common fields
	var commonFields []slog.Attr
	for k, v := range cfg.CommonFields {
		commonFields = append(commonFields, slog.String(k, v))
	}

	// Create new module config
	moduleConfig := NewModuleConfig(defaultLevel)
	for _, mod := range cfg.Modules {
		level := ParseLevel(mod.Level)
		moduleConfig.SetModuleLevel(mod.Name, level)
	}

	// Update global module config
	globalModuleConfig = moduleConfig

	// Determine format
	useJSON := cfg.Format == FormatJSON

	// Reinitialize logger with new configuration
	initLoggerWithConfig(defaultLevel, commonFields, moduleConfig, useJSON)

	return nil
}

// initLoggerWithConfig creates the logger with full configuration.
func initLoggerWithConfig(level slog.Level, commonFields []slog.Attr, moduleConfig *ModuleConfig, useJSON bool) {
	var baseHandler slog.Handler
	opts := &slog.HandlerOptions{
		Level: level,
	}

	if useJSON {
		baseHandler = slog.NewJSONHandler(logOutput, opts)
	} else {
		baseHandler = slog.NewTextHandler(logOutput, opts)
	}

	// Wrap with module-aware handler if we have module config
	var handler slog.Handler
	if moduleConfig != nil && len(moduleConfig.modules) > 0 {
		handler = NewModuleHandler(baseHandler, moduleConfig, commonFields...)
	} else {
		handler = NewContextHandler(baseHandler, commonFields...)
	}

	DefaultLogger = slog.New(handler)
	slog.SetDefault(DefaultLogger)
}

// GetModuleConfig returns the global module configuration.
// This is primarily for testing.
func GetModuleConfig() *ModuleConfig {
	return globalModuleConfig
}
