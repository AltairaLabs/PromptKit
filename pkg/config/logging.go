package config

// LoggingConfig represents logging configuration in K8s-style manifest format.
// This is used for schema generation and external configuration files.
type LoggingConfig struct {
	//nolint:lll // jsonschema tags require single line
	APIVersion string `yaml:"apiVersion" jsonschema:"const=promptkit.altairalabs.ai/v1alpha1,title=API Version,description=Schema version identifier"`
	//nolint:lll // jsonschema tags require single line
	Kind     string            `yaml:"kind" jsonschema:"const=LoggingConfig,title=Kind,description=Resource type identifier"`
	Metadata ObjectMeta        `yaml:"metadata,omitempty" jsonschema:"title=Metadata,description=Resource metadata"`
	Spec     LoggingConfigSpec `yaml:"spec" jsonschema:"title=Spec,description=Logging configuration specification"`
}

// LoggingConfigSpec defines the logging configuration parameters.
type LoggingConfigSpec struct {
	// DefaultLevel is the default log level for all modules.
	// Supported values: trace, debug, info, warn, error.
	//nolint:lll // jsonschema tags require single line
	DefaultLevel string `yaml:"defaultLevel,omitempty" jsonschema:"enum=trace,enum=debug,enum=info,enum=warn,enum=error,default=info,title=Default Level,description=Default log level for all modules"`

	// Format specifies the output format.
	// "json" produces machine-parseable JSON logs.
	// "text" produces human-readable text logs.
	//nolint:lll // jsonschema tags require single line
	Format string `yaml:"format,omitempty" jsonschema:"enum=json,enum=text,default=text,title=Format,description=Log output format"`

	// CommonFields are key-value pairs added to every log entry.
	// Useful for environment, service name, cluster, etc.
	//nolint:lll // jsonschema tags require single line
	CommonFields map[string]string `yaml:"commonFields,omitempty" jsonschema:"title=Common Fields,description=Key-value pairs added to every log entry"`

	// Modules configures logging for specific modules.
	// Module names use dot notation (e.g., runtime.pipeline).
	//nolint:lll // jsonschema tags require single line
	Modules []ModuleLoggingConfig `yaml:"modules,omitempty" jsonschema:"title=Modules,description=Per-module logging configuration"`
}

// ModuleLoggingConfig configures logging for a specific module.
type ModuleLoggingConfig struct {
	// Name is the module name pattern using dot notation.
	// Examples: "runtime", "runtime.pipeline", "providers.openai".
	// More specific names take precedence over less specific ones.
	//nolint:lll // jsonschema tags require single line
	Name string `yaml:"name" jsonschema:"title=Name,description=Module name pattern using dot notation (e.g. runtime.pipeline)"`

	// Level is the log level for this module.
	// Overrides the default level for matching loggers.
	//nolint:lll // jsonschema tags require single line
	Level string `yaml:"level" jsonschema:"enum=trace,enum=debug,enum=info,enum=warn,enum=error,title=Level,description=Log level for this module"`

	// Fields are additional key-value pairs added to logs from this module.
	//nolint:lll // jsonschema tags require single line
	Fields map[string]string `yaml:"fields,omitempty" jsonschema:"title=Fields,description=Additional fields added to logs from this module"`
}

// LogLevel constants for programmatic use.
const (
	LogLevelTrace = "trace"
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

// LogFormat constants for programmatic use.
const (
	LogFormatJSON = "json"
	LogFormatText = "text"
)

// DefaultLoggingConfig returns a LoggingConfigSpec with sensible defaults.
func DefaultLoggingConfig() LoggingConfigSpec {
	return LoggingConfigSpec{
		DefaultLevel: LogLevelInfo,
		Format:       LogFormatText,
	}
}

// Validate validates the LoggingConfigSpec.
func (c *LoggingConfigSpec) Validate() error {
	// Validate default level
	if c.DefaultLevel != "" && !isValidLogLevel(c.DefaultLevel) {
		return &ValidationError{
			Field:   "defaultLevel",
			Message: "must be one of: trace, debug, info, warn, error",
			Value:   c.DefaultLevel,
		}
	}

	// Validate format
	if c.Format != "" && c.Format != LogFormatJSON && c.Format != LogFormatText {
		return &ValidationError{
			Field:   "format",
			Message: "must be one of: json, text",
			Value:   c.Format,
		}
	}

	// Validate module configs
	for i, mod := range c.Modules {
		if mod.Name == "" {
			return &ValidationError{
				Field:   "modules[" + string(rune('0'+i)) + "].name",
				Message: "module name is required",
			}
		}
		if mod.Level != "" && !isValidLogLevel(mod.Level) {
			return &ValidationError{
				Field:   "modules[" + mod.Name + "].level",
				Message: "must be one of: trace, debug, info, warn, error",
				Value:   mod.Level,
			}
		}
	}

	return nil
}

// isValidLogLevel checks if a log level string is valid.
func isValidLogLevel(level string) bool {
	switch level {
	case LogLevelTrace, LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
		return true
	default:
		return false
	}
}

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
	Value   string
}

func (e *ValidationError) Error() string {
	if e.Value != "" {
		return "logging config validation error: " + e.Field + ": " + e.Message + " (got: " + e.Value + ")"
	}
	return "logging config validation error: " + e.Field + ": " + e.Message
}
