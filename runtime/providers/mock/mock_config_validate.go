package mock

import (
	"fmt"
	"strings"
)

// ConfigProblem is one thing wrong with a mock config file.
type ConfigProblem struct {
	// Line is the 1-based line of the offending key or syntax error, or 0
	// when the decoder did not report one.
	Line int
	// Path is where in the config the problem is, such as
	// "scenarios.s1.turns.1.tool_calls[0]", or "" for one found by the YAML
	// decoder, which reports a line instead.
	Path string
	// Message says what is wrong, such as `unknown key "args" (valid:
	// arguments, name)`.
	Message string
}

// ConfigError lists every problem ValidateConfig found.
type ConfigError struct {
	Problems []ConfigProblem
}

// Error joins the problems, each prefixed with its path, or its line when it
// has no path.
func (e *ConfigError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		switch {
		case p.Path != "":
			parts[i] = p.Path + ": " + p.Message
		case p.Line > 0:
			parts[i] = fmt.Sprintf("line %d: %s", p.Line, p.Message)
		default:
			parts[i] = p.Message
		}
	}
	return strings.Join(parts, "; ")
}

// ValidateConfig reports what NewFileMockRepository would reject in a mock
// config file's contents: a YAML error, or every key the repository does not
// read, with the keys valid at that place. It returns nil for a valid config,
// and a *ConfigError otherwise.
func ValidateConfig(data []byte) error {
	if _, err := decodeConfigStrict(data); err != nil {
		return err
	}
	return nil
}
