package mock

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
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
var unknownFieldRe = regexp.MustCompile(`^field (\S+) not found in type mock\.(\w+)$`)

// decoderLineRe matches the line yaml.v3 puts at the front of an error:
// "line 3: ..." in a type error, "yaml: line 3: ..." in a syntax error.
var decoderLineRe = regexp.MustCompile(`^(?:yaml: )?line (\d+): (.*)$`)

// decodeConfigStrict decodes a mock config file, rejecting any key the
// repository does not read. A misspelled key used to be dropped silently:
// `default_response:` for `defaultResponse:` left the provider answering with
// its generic text. Every unknown key is reported with the keys valid at that
// place, as a *ConfigError. An empty file is an empty config.
func decodeConfigStrict(data []byte) (*Config, error) {
	var config Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&config); err != nil && !errors.Is(err, io.EOF) {
		return nil, &ConfigError{Problems: describeDecodeError(err)}
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, &ConfigError{Problems: describeDecodeError(err)}
	}
	if problems := checkResponses(&doc); len(problems) > 0 {
		return nil, &ConfigError{Problems: problems}
	}
	return &config, nil
}

// describeDecodeError turns a yaml.v3 error into problems on their lines,
// naming the valid keys for an unknown one.
func describeDecodeError(err error) []ConfigProblem {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return []ConfigProblem{decoderProblem(err.Error())}
	}
	problems := make([]ConfigProblem, 0, len(typeErr.Errors))
	for _, line := range typeErr.Errors {
		problem := decoderProblem(line)
		if m := unknownFieldRe.FindStringSubmatch(problem.Message); m != nil {
			problem.Message = fmt.Sprintf("unknown key %q (valid: %s)", m[1], strings.Join(yamlKeysOf(m[2]), ", "))
		}
		problems = append(problems, problem)
	}
	return problems
}

// decoderProblem splits one yaml.v3 error line into its line and message.
func decoderProblem(text string) ConfigProblem {
	m := decoderLineRe.FindStringSubmatch(text)
	if m == nil {
		return ConfigProblem{Message: text}
	}
	line, _ := strconv.Atoi(m[1])
	return ConfigProblem{Line: line, Message: m[2]}
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

// checkResponses checks every structured turn and step response in the
// document, in document order. The decoder has already checked the keys
// above them, so the shape down to the responses is known.
func checkResponses(doc *yaml.Node) []ConfigProblem {
	root := documentRoot(doc)
	var problems []ConfigProblem
	for _, section := range []string{"scenarios", "selfplay"} {
		forEachEntry(mappingValue(root, section), func(name string, scenario *yaml.Node) {
			for _, kind := range []string{"turns", "steps"} {
				forEachEntry(mappingValue(scenario, kind), func(key string, response *yaml.Node) {
					where := fmt.Sprintf("%s.%s.%s.%s", section, name, kind, key)
					problems = append(problems, checkResponseKeys(where, response)...)
				})
			}
		})
	}
	return problems
}

// checkResponseKeys checks one turn or step response. A plain string is a text
// response and has no keys; a mapping must use the keys the repository reads.
func checkResponseKeys(where string, response *yaml.Node) []ConfigProblem {
	problems := unknownKeys(where, response, mockTurnKeys)
	for i, call := range sequenceItems(mappingValue(response, turnTypeToolCalls)) {
		problems = append(problems, unknownKeys(fmt.Sprintf("%s.tool_calls[%d]", where, i), call, mockToolCallKeys)...)
	}
	for i, part := range sequenceItems(mappingValue(response, "parts")) {
		problems = append(problems, unknownKeys(fmt.Sprintf("%s.parts[%d]", where, i), part, mockPartKeys)...)
	}
	return problems
}

// unknownKeys reports each key of a mapping node that is not allowed, on the
// key's line.
func unknownKeys(where string, node *yaml.Node, allowed []string) []ConfigProblem {
	var out []ConfigProblem
	forEachEntryKey(node, func(key *yaml.Node, _ *yaml.Node) {
		if !slices.Contains(allowed, key.Value) {
			out = append(out, ConfigProblem{Line: key.Line, Path: where, Message: fmt.Sprintf(
				"unknown key %q (valid: %s)", key.Value, strings.Join(allowed, ", "))})
		}
	})
	return out
}

// documentRoot is the top node of a decoded document, or nil for an empty one.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return nil
}

// mappingValue is the value under key in a mapping node, or nil.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	var found *yaml.Node
	forEachEntryKey(node, func(k, v *yaml.Node) {
		if found == nil && k.Value == key {
			found = v
		}
	})
	return found
}

// forEachEntry calls fn with each key and value of a mapping node.
func forEachEntry(node *yaml.Node, fn func(key string, value *yaml.Node)) {
	forEachEntryKey(node, func(k, v *yaml.Node) { fn(k.Value, v) })
}

// forEachEntryKey calls fn with each key node and value node of a mapping node.
func forEachEntryKey(node *yaml.Node, fn func(key, value *yaml.Node)) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		fn(node.Content[i], node.Content[i+1])
	}
}

// sequenceItems is the items of a sequence node, or nil.
func sequenceItems(node *yaml.Node) []*yaml.Node {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	return node.Content
}
