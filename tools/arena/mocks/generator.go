// Package mocks converts Arena run results into mock provider configurations.
package mocks

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// WriteOptions controls how mock YAML files are emitted.
type WriteOptions struct {
	// OutputPath controls the destination. If PerScenario is false, this is the file path.
	// If PerScenario is true, this is the directory that will contain one file per scenario.
	OutputPath      string
	PerScenario     bool
	Merge           bool
	DefaultResponse string
	DryRun          bool
}

// WriteFiles renders the provided File into YAML bytes and optionally writes them to disk.
// It returns a map of output path -> YAML bytes (useful for dry-run and testing).
func WriteFiles(file File, opts WriteOptions) (map[string][]byte, error) {
	if opts.OutputPath == "" && !opts.DryRun {
		return nil, fmt.Errorf("output path is required")
	}

	if opts.DefaultResponse != "" && file.DefaultResponse == "" {
		file.DefaultResponse = opts.DefaultResponse
	}

	if opts.PerScenario {
		return writePerScenario(file, opts)
	}
	return writeSingle(file, opts)
}

func writeSingle(file File, opts WriteOptions) (map[string][]byte, error) {
	outPath := opts.OutputPath
	var base File

	if opts.Merge && outPath != "" {
		var err error
		base, err = loadFileIfExists(outPath)
		if err != nil {
			return nil, err
		}
	}

	merged := mergeFiles(base, file)
	yamlBytes, err := renderYAML(merged)
	if err != nil {
		return nil, err
	}

	if !opts.DryRun {
		if err := os.MkdirAll(filepath.Dir(outPath), dirPerm); err != nil {
			return nil, fmt.Errorf("failed to create output dir: %w", err)
		}
		if err := os.WriteFile(outPath, yamlBytes, filePerm); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", outPath, err)
		}
	}

	return map[string][]byte{outPath: yamlBytes}, nil
}

func writePerScenario(file File, opts WriteOptions) (map[string][]byte, error) { //nolint:gocognit
	if opts.OutputPath == "" {
		return nil, fmt.Errorf("output directory is required for per-scenario output")
	}

	if !opts.DryRun {
		if err := os.MkdirAll(opts.OutputPath, dirPerm); err != nil {
			return nil, fmt.Errorf("failed to create output dir: %w", err)
		}
	}

	results := make(map[string][]byte)

	for _, name := range sortedScenarioNames(file.Scenarios) {
		outPath := filepath.Join(opts.OutputPath, fmt.Sprintf("%s.yaml", name))
		yamlBytes, err := renderScenarioFile(outPath, file, name, opts)
		if err != nil {
			return nil, err
		}
		results[outPath] = yamlBytes
	}

	return results, nil
}

func sortedScenarioNames(scenarios map[string]ScenarioTurnHistory) []string {
	names := make([]string, 0, len(scenarios))
	for name := range scenarios {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func renderScenarioFile(outPath string, file File, name string, opts WriteOptions) ([]byte, error) {
	scenarioOnly := File{
		DefaultResponse: file.DefaultResponse,
		Scenarios: map[string]ScenarioTurnHistory{
			name: file.Scenarios[name],
		},
	}

	if opts.Merge {
		base, err := loadFileIfExists(outPath)
		if err != nil {
			return nil, err
		}
		scenarioOnly = mergeFiles(base, scenarioOnly)
	}

	yamlBytes, err := renderYAML(scenarioOnly)
	if err != nil {
		return nil, err
	}

	if opts.DryRun {
		return yamlBytes, nil
	}

	if err := os.WriteFile(outPath, yamlBytes, filePerm); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", outPath, err)
	}
	return yamlBytes, nil
}

// mergeFiles overlays newFile onto base. New scenarios/turns override existing ones.
func mergeFiles(base, newFile File) File {
	result := File{
		DefaultResponse: base.DefaultResponse,
		Scenarios:       map[string]ScenarioTurnHistory{},
	}

	if newFile.DefaultResponse != "" {
		result.DefaultResponse = newFile.DefaultResponse
	}

	// Start with base scenarios
	for k, v := range base.Scenarios {
		result.Scenarios[k] = v
	}

	// Overlay with new scenarios/turns
	for k, v := range newFile.Scenarios {
		if existing, ok := result.Scenarios[k]; ok {
			result.Scenarios[k] = mergeScenario(existing, v)
		} else {
			result.Scenarios[k] = v
		}
	}

	return result
}

func mergeScenario(base, incoming ScenarioTurnHistory) ScenarioTurnHistory {
	merged := ScenarioTurnHistory{
		DefaultResponse: base.DefaultResponse,
		Turns:           map[int]TurnTemplate{},
	}

	if incoming.DefaultResponse != "" {
		merged.DefaultResponse = incoming.DefaultResponse
	}

	for k, v := range base.Turns {
		merged.Turns[k] = v
	}
	for k, v := range incoming.Turns {
		merged.Turns[k] = v
	}

	return sortTurns(merged)
}

func renderYAML(file File) ([]byte, error) {
	root := yaml.Node{
		Kind: yaml.MappingNode,
	}

	if file.DefaultResponse != "" {
		root.Content = append(root.Content,
			scalarNode("defaultResponse"),
			scalarNode(file.DefaultResponse),
		)
	}

	scenarioNames := make([]string, 0, len(file.Scenarios))
	for name := range file.Scenarios {
		scenarioNames = append(scenarioNames, name)
	}
	sort.Strings(scenarioNames)

	if len(scenarioNames) > 0 {
		scenariosNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, name := range scenarioNames {
			hist := file.Scenarios[name]
			scNode, err := scenarioToNode(hist)
			if err != nil {
				return nil, err
			}
			scenariosNode.Content = append(scenariosNode.Content, scalarNode(name), scNode)
		}
		root.Content = append(root.Content, scalarNode("scenarios"), scenariosNode)
	}

	encoded, err := yaml.Marshal(&root)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal yaml: %w", err)
	}
	return encoded, nil
}

func scenarioToNode(hist ScenarioTurnHistory) (*yaml.Node, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}

	if hist.DefaultResponse != "" {
		node.Content = append(node.Content, scalarNode("defaultResponse"), scalarNode(hist.DefaultResponse))
	}

	turnNumbers := make([]int, 0, len(hist.Turns))
	for n := range hist.Turns {
		turnNumbers = append(turnNumbers, n)
	}
	sort.Ints(turnNumbers)

	if len(turnNumbers) > 0 {
		turnsNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, n := range turnNumbers {
			turnNode, err := encodeNode(hist.Turns[n])
			if err != nil {
				return nil, fmt.Errorf("turn %d: %w", n, err)
			}
			turnsNode.Content = append(turnsNode.Content, intScalarNode(n), turnNode)
		}
		node.Content = append(node.Content, scalarNode("turns"), turnsNode)
	}

	return node, nil
}

func encodeNode(v interface{}) (*yaml.Node, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: ""}, nil
	}
	return doc.Content[0], nil
}

func scalarNode(val string) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: val,
		Tag:   "!!str",
	}
}

func intScalarNode(val int) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: strconv.Itoa(val),
		Tag:   "!!int",
	}
}

func loadFileIfExists(path string) (File, error) {
	var file File
	data, err := os.ReadFile(path) //nolint:gosec // reading user-specified path is expected
	if err != nil {
		if os.IsNotExist(err) {
			return file, nil
		}
		return file, fmt.Errorf("failed to read existing file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &file); err != nil {
		return File{}, fmt.Errorf("failed to parse existing file %s: %w", path, err)
	}
	return file, nil
}
