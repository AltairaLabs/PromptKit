package mock

import (
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
)

// AgentConfig is the YAML-friendly configuration for a mock agent.
type AgentConfig struct {
	Name      string        `json:"name" yaml:"name"`
	Card      a2a.AgentCard `json:"card" yaml:"card"`
	Responses []RuleConfig  `json:"responses" yaml:"responses"`
}

// RuleConfig describes a single response rule.
type RuleConfig struct {
	Skill    string          `json:"skill" yaml:"skill"`
	Match    *MatchConfig    `json:"match,omitempty" yaml:"match,omitempty"`
	Response *ResponseConfig `json:"response,omitempty" yaml:"response,omitempty"`
	Error    string          `json:"error,omitempty" yaml:"error,omitempty"`
}

// MatchConfig describes how to match incoming messages.
type MatchConfig struct {
	Contains string `json:"contains,omitempty" yaml:"contains,omitempty"`
	Regex    string `json:"regex,omitempty" yaml:"regex,omitempty"`
}

// ResponseConfig describes the response parts.
type ResponseConfig struct {
	Parts []PartConfig `json:"parts" yaml:"parts"`
}

// PartConfig describes a single response part.
type PartConfig struct {
	Text string `json:"text" yaml:"text"`
}

// OptionsFromConfig converts an AgentConfig's response rules into Options.
func OptionsFromConfig(cfg *AgentConfig) []Option {
	var opts []Option
	for _, rule := range cfg.Responses {
		if rule.Error != "" {
			opts = append(opts, WithSkillError(rule.Skill, rule.Error))
			continue
		}
		if rule.Response == nil {
			continue
		}

		resp := configToResponse(rule.Response)
		matcher := matcherFromConfig(rule.Match)

		if matcher != nil {
			opts = append(opts, WithInputMatcher(rule.Skill, matcher, resp))
		} else {
			opts = append(opts, WithSkillResponse(rule.Skill, resp))
		}
	}
	return opts
}

// configToResponse converts a ResponseConfig to a Response.
func configToResponse(cfg *ResponseConfig) Response {
	var parts []a2a.Part
	for _, p := range cfg.Parts {
		text := p.Text
		parts = append(parts, a2a.Part{Text: &text})
	}
	return Response{Parts: parts}
}

// matcherFromConfig builds a matcher function from a MatchConfig.
// Returns nil if mc is nil (no matching required).
func matcherFromConfig(mc *MatchConfig) func(a2a.Message) bool {
	if mc == nil {
		return nil
	}

	var checks []func(string) bool

	if mc.Contains != "" {
		lower := strings.ToLower(mc.Contains)
		checks = append(checks, func(text string) bool {
			return strings.Contains(strings.ToLower(text), lower)
		})
	}

	if mc.Regex != "" {
		re := regexp.MustCompile(mc.Regex)
		checks = append(checks, func(text string) bool {
			return re.MatchString(text)
		})
	}

	if len(checks) == 0 {
		return nil
	}

	return func(msg a2a.Message) bool {
		text := messageText(&msg)
		for _, check := range checks {
			if !check(text) {
				return false
			}
		}
		return true
	}
}
