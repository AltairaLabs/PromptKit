package handlers

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/jmespath/go-jmespath"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// toolExecConfig is a tool_exec check's parsed params: the output gates
// and the optional score extraction.
type toolExecConfig struct {
	success *regexp.Regexp
	failure *regexp.Regexp
	score   *toolExecScore
}

// parseToolExecConfig compiles every pattern up front, so a bad regex or
// path is a configuration error and never costs a tool call.
func parseToolExecConfig(params map[string]any) (*toolExecConfig, string) {
	if msg := rejectThresholdParams(params); msg != "" {
		return nil, "tool_exec: " + msg
	}
	cfg := &toolExecConfig{}
	for _, gate := range []struct {
		key string
		dst **regexp.Regexp
	}{{"success_pattern", &cfg.success}, {"failure_pattern", &cfg.failure}} {
		pattern, _ := params[gate.key].(string)
		if pattern == "" {
			continue
		}
		rx, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Sprintf("tool_exec: %s %q is not a valid regex: %v", gate.key, pattern, err)
		}
		*gate.dst = rx
	}
	score, msg := parseToolExecScore(params)
	if msg != "" {
		return nil, msg
	}
	cfg.score = score
	return cfg, ""
}

// failFunc returns how this check reports a failed call. Without score
// extraction a failed call IS the measurement (0). With it, no number was
// taken, so the result is an Error: scoring 0 would pass a max_score gate.
func (c *toolExecConfig) failFunc(handlerType string) func(string) *evals.EvalResult {
	if c.score != nil {
		return func(msg string) *evals.EvalResult { return errorResult(handlerType, msg) }
	}
	return func(msg string) *evals.EvalResult { return failResult(handlerType, msg) }
}

// toolExecScore is the numeric-score extraction a tool_exec check asks for:
// a JMESPath into the parsed tool payload (score_path) or a one-group regex
// over the tool's text output (score_pattern), divided by score_scale.
type toolExecScore struct {
	path    *jmespath.JMESPath
	expr    string // score_path as written, for explanations
	pattern *regexp.Regexp
	scale   float64
}

// parseToolExecScore reads score_path / score_pattern / score_scale. It
// returns a nil spec when neither extractor is set (the binary score
// applies), and a non-empty message for a misconfiguration the pack author
// can fix.
func parseToolExecScore(params map[string]any) (spec *toolExecScore, errMsg string) {
	path, _ := params["score_path"].(string)
	pattern, _ := params["score_pattern"].(string)
	_, hasScale := params["score_scale"]

	if path == "" && pattern == "" {
		if hasScale {
			return nil, "tool_exec: score_scale needs score_path or score_pattern"
		}
		return nil, ""
	}
	if path != "" && pattern != "" {
		return nil, "tool_exec: set score_path or score_pattern, not both"
	}

	spec = &toolExecScore{scale: 1}
	if hasScale {
		scale, ok := extractFloat64(params, "score_scale")
		if !ok || scale <= 0 || !isFinite(scale) {
			return nil, fmt.Sprintf("tool_exec: score_scale must be a positive number, got %v", params["score_scale"])
		}
		spec.scale = scale
	}

	if path != "" {
		compiled, err := jmespath.Compile(path)
		if err != nil {
			return nil, fmt.Sprintf("tool_exec: score_path %q is not a valid JMESPath expression: %v", path, err)
		}
		spec.path = compiled
		spec.expr = path
		return spec, ""
	}

	rx, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Sprintf("tool_exec: score_pattern %q is not a valid regex: %v", pattern, err)
	}
	if rx.NumSubexp() != 1 {
		return nil, fmt.Sprintf("tool_exec: score_pattern %q must have exactly one capture group, has %d",
			pattern, rx.NumSubexp())
	}
	spec.pattern = rx
	return spec, ""
}

// extract returns the raw (unscaled) value, or a non-empty explanation when
// the tool's output does not carry one.
func (s *toolExecScore) extract(payload any, outputText string) (raw float64, miss string) {
	if s.path != nil {
		return s.extractPath(payload)
	}
	return s.extractPattern(outputText)
}

func (s *toolExecScore) extractPath(payload any) (float64, string) {
	expr := s.expr
	switch payload.(type) {
	case map[string]any, []any:
	default:
		return 0, fmt.Sprintf("score_path %q found no value: tool output is not a JSON object or array "+
			"(use score_pattern for text output)", expr)
	}
	value, err := s.path.Search(payload)
	if err != nil || value == nil {
		return 0, fmt.Sprintf("score_path %q found no value in the tool output", expr)
	}
	switch v := value.(type) {
	case float64:
		return v, ""
	case string:
		// Shell-wrapped tools often print numbers as JSON strings.
		if n, parseErr := strconv.ParseFloat(strings.TrimSpace(v), 64); parseErr == nil && isFinite(n) {
			return n, ""
		}
	}
	return 0, fmt.Sprintf("score_path %q selected %v (%T), which is not a number", expr, value, value)
}

func (s *toolExecScore) extractPattern(outputText string) (float64, string) {
	m := s.pattern.FindStringSubmatch(outputText)
	if m == nil {
		return 0, fmt.Sprintf("score_pattern %q did not match the tool output", s.pattern.String())
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
	if err != nil || !isFinite(n) {
		return 0, fmt.Sprintf("score_pattern %q captured %q, which is not a number", s.pattern.String(), m[1])
	}
	return n, ""
}

func isFinite(f float64) bool { return !math.IsInf(f, 0) && !math.IsNaN(f) }
