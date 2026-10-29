package handlers

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/jmespath/go-jmespath"
)

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
		if !ok || scale <= 0 || math.IsInf(scale, 0) || math.IsNaN(scale) {
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
	if payload == nil {
		return 0, fmt.Sprintf("score_path %q found no value: tool output is not JSON", expr)
	}
	value, err := s.path.Search(payload)
	if err != nil || value == nil {
		return 0, fmt.Sprintf("score_path %q found no value in the tool output", expr)
	}
	n, ok := value.(float64)
	if !ok {
		return 0, fmt.Sprintf("score_path %q selected %v, which is not a number", expr, value)
	}
	return n, ""
}

func (s *toolExecScore) extractPattern(outputText string) (float64, string) {
	m := s.pattern.FindStringSubmatch(outputText)
	if m == nil {
		return 0, fmt.Sprintf("score_pattern %q did not match the tool output", s.pattern.String())
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, fmt.Sprintf("score_pattern %q captured %q, which is not a number", s.pattern.String(), m[1])
	}
	return n, ""
}
