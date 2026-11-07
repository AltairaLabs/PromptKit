package mock

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Keys the repository reads from a structured turn (or step) response, a tool
// call and a content part: parseStructuredResponse, parseToolCalls and
// parseSinglePart. A turn's text is read from content, text or the legacy
// response key.
const keyType = "type"

var (
	mockTurnKeys = []string{
		"audio_file", "audio_mime_type", "audio_sample_rate", "content", "parts", "response",
		turnTypeText, turnTypeToolCalls, keyType,
	}
	mockToolCallKeys = []string{"arguments", "name"}
	mockPartKeys     = []string{"audio_url", "image_url", "metadata", turnTypeText, keyType, "video_url"}
)

// unknownFieldRe matches yaml.v3's report of a key with no matching field.
var unknownFieldRe = regexp.MustCompile(`^(line \d+): field (\S+) not found in type mock\.(\w+)$`)

// decodeConfigStrict decodes a mock config file, rejecting any key the
// repository does not read. A misspelled key used to be dropped silently:
// `default_response:` for `defaultResponse:` left the provider answering with
// its generic text. Every unknown key is reported with the keys valid at that
// place. An empty file is an empty config.
func decodeConfigStrict(data []byte) (*Config, error) {
	var config Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&config); err != nil && !errors.Is(err, io.EOF) {
		return nil, describeDecodeError(err)
	}

	var problems []string
	for _, section := range []struct {
		name    string
		entries map[string]ScenarioConfig
	}{{"scenarios", config.Scenarios}, {"selfplay", config.Selfplay}} {
		for _, key := range slices.Sorted(maps.Keys(section.entries)) {
			sc := section.entries[key]
			for _, turn := range slices.Sorted(maps.Keys(sc.Turns)) {
				where := fmt.Sprintf("%s.%s.turns.%d", section.name, key, turn)
				problems = append(problems, checkResponseKeys(where, sc.Turns[turn])...)
			}
			for _, step := range slices.Sorted(maps.Keys(sc.Steps)) {
				where := fmt.Sprintf("%s.%s.steps.%s", section.name, key, step)
				problems = append(problems, checkResponseKeys(where, sc.Steps[step])...)
			}
		}
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return &config, nil
}

// describeDecodeError rewrites yaml.v3's unknown-field lines to name the
// valid keys. Other decode errors pass through unchanged.
func describeDecodeError(err error) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	lines := make([]string, 0, len(typeErr.Errors))
	for _, line := range typeErr.Errors {
		m := unknownFieldRe.FindStringSubmatch(line)
		if m == nil {
			lines = append(lines, line)
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: unknown key %q (valid: %s)",
			m[1], m[2], strings.Join(yamlKeysOf(m[3]), ", ")))
	}
	return errors.New(strings.Join(lines, "; "))
}

// yamlKeysOf lists the yaml keys of the named config struct.
func yamlKeysOf(typeName string) []string {
	var t reflect.Type
	switch typeName {
	case "Config":
		t = reflect.TypeOf(Config{})
	case "ScenarioConfig":
		t = reflect.TypeOf(ScenarioConfig{})
	default:
		return nil
	}
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}

// checkResponseKeys checks one turn or step response. A plain string is a text
// response; a map must use the keys the repository reads.
func checkResponseKeys(where string, response any) []string {
	m, ok := response.(map[string]any)
	if !ok {
		return nil
	}
	problems := unknownResponseKeys(where, m, mockTurnKeys)
	if calls, ok := m["tool_calls"].([]any); ok {
		for i, call := range calls {
			if cm, ok := call.(map[string]any); ok {
				problems = append(problems,
					unknownResponseKeys(fmt.Sprintf("%s.tool_calls[%d]", where, i), cm, mockToolCallKeys)...)
			}
		}
	}
	if parts, ok := m["parts"].([]any); ok {
		for i, part := range parts {
			if pm, ok := part.(map[string]any); ok {
				problems = append(problems,
					unknownResponseKeys(fmt.Sprintf("%s.parts[%d]", where, i), pm, mockPartKeys)...)
			}
		}
	}
	return problems
}

func unknownResponseKeys(where string, m map[string]any, allowed []string) []string {
	var out []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if !slices.Contains(allowed, k) {
			out = append(out, fmt.Sprintf("%s: unknown key %q (valid: %s)", where, k, strings.Join(allowed, ", ")))
		}
	}
	return out
}
