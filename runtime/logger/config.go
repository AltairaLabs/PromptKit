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
	fields       map[string][]slog.Attr
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

// SetModuleFields sets attributes that are added to every record logged by
// the module and its children. A child's field overrides a parent's field
// with the same key. Passing no attributes removes the module's fields.
func (m *ModuleConfig) SetModuleFields(module string, attrs ...slog.Attr) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(attrs) == 0 {
		delete(m.fields, module)
		return
	}
	if m.fields == nil {
		m.fields = make(map[string][]slog.Attr)
	}
	m.fields[module] = append([]slog.Attr(nil), attrs...)
}

// FieldsFor returns the attributes configured for the module and its
// ancestors, ordered from the most general module to the most specific. When
// two modules set the same key, only the more specific module's attribute is
// returned.
func (m *ModuleConfig) FieldsFor(module string) []slog.Attr {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.fields) == 0 || module == "" {
		return nil
	}

	// Collect the module and its ancestors, most specific first.
	var chain []string
	for name := module; ; {
		chain = append(chain, name)
		lastDot := strings.LastIndex(name, ".")
		if lastDot == -1 {
			break
		}
		name = name[:lastDot]
	}

	var out []slog.Attr
	seen := make(map[string]bool)
	for _, name := range chain {
		for _, attr := range m.fields[name] {
			if !seen[attr.Key] {
				seen[attr.Key] = true
				out = append(out, attr)
			}
		}
	}
	// Reverse so the most general module's fields come first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// hasOverrides reports whether any module sets a level or fields.
func (m *ModuleConfig) hasOverrides() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.modules) > 0 || len(m.fields) > 0
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
// It mirrors the config package's LoggingConfigSpec, which the runtime cannot
// import.
type LoggingConfigSpec struct {
	// DefaultLevel is the log level name (see ParseLevel). Empty means "info".
	DefaultLevel string
	// Format is "json" or "text". Any other value selects text.
	Format string
	// CommonFields are attached to every log record.
	CommonFields map[string]string
	// Modules sets per-module log levels.
	Modules []ModuleLoggingSpec
}

// ModuleLoggingSpec configures logging for a specific module.
type ModuleLoggingSpec struct {
	// Name is the dotted module name, such as "runtime.pipeline".
	Name string
	// Level is the log level name for this module and its children.
	Level string
	// Fields are attached to every record logged by this module and its
	// children. A child module's field overrides a parent's field with the
	// same key.
	Fields map[string]string
}

// Log format constants
const (
	FormatJSON = "json"
	FormatText = "text"
)

// Configure applies a LoggingConfigSpec to the global logger, replacing the
// current logger with one built from the spec. The level, format, common
// fields and module settings are recorded, so a later SetLevel, SetVerbose,
// SetOutput or SetLogger(nil) rebuilds the logger with them and changes only
// what it names. A nil spec does nothing, and so does any call made while a
// logger installed by SetLogger is in place.
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

	// Create new module config
	moduleConfig := NewModuleConfig(defaultLevel)
	for _, mod := range cfg.Modules {
		moduleConfig.SetModuleLevel(mod.Name, ParseLevel(mod.Level))
		moduleConfig.SetModuleFields(mod.Name, stringAttrs(mod.Fields)...)
	}

	// Record the configuration so later rebuilds keep it.
	globalModuleConfig = moduleConfig
	currentLevel = defaultLevel
	currentFormat = FormatText
	if cfg.Format == FormatJSON {
		currentFormat = FormatJSON
	}
	currentCommonFields = stringAttrs(cfg.CommonFields)

	initLogger(currentLevel)

	return nil
}

// stringAttrs converts a string map into attributes sorted by key, so the
// order of the fields in a record does not change between runs.
func stringAttrs(fields map[string]string) []slog.Attr {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	attrs := make([]slog.Attr, 0, len(keys))
	for _, k := range keys {
		attrs = append(attrs, slog.String(k, fields[k]))
	}
	return attrs
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
	if moduleConfig != nil && moduleConfig.hasOverrides() {
		handler = NewModuleHandler(baseHandler, moduleConfig, commonFields...)
	} else {
		handler = NewContextHandler(baseHandler, commonFields...)
	}

	storeLogger(slog.New(handler))
}

// GetModuleConfig returns the global module configuration.
// This is primarily for testing.
func GetModuleConfig() *ModuleConfig {
	return globalModuleConfig
}
