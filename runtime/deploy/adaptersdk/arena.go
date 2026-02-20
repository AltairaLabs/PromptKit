package adaptersdk

import (
	"encoding/json"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// arenaToolSpec mirrors the tool-relevant fields from config.ToolSpec.
type arenaToolSpec struct {
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
	Mode        string      `json:"mode"`
	HTTP        *arenaHTTP  `json:"http,omitempty"`
}

type arenaHTTP struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

// arenaToolData mirrors config.ToolData for JSON deserialization.
type arenaToolData struct {
	FilePath string `json:"file_path,omitempty"`
	Data     []byte `json:"data,omitempty"`
}

// arenaConfig is a lightweight subset of config.Config for adapter use.
type arenaConfig struct {
	LoadedTools []arenaToolData           `json:"loaded_tools,omitempty"`
	ToolSpecs   map[string]*arenaToolSpec `json:"tool_specs,omitempty"`
}

// toolManifest represents the YAML structure of a .tool.yaml file.
type toolManifest struct {
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec toolManifestSpec `yaml:"spec"`
}

type toolManifestSpec struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	InputSchema interface{} `yaml:"input_schema"`
	Mode        string      `yaml:"mode"`
	HTTP        *arenaHTTP  `yaml:"http,omitempty"`
}

// arenaScenario is a lightweight subset of config.Scenario.
type arenaScenario struct {
	ToolPolicy *arenaToolPolicy `json:"tool_policy,omitempty"`
}

type arenaToolPolicy struct {
	Blocklist []string `json:"blocklist,omitempty"`
}

type arenaConfigWithScenarios struct {
	LoadedScenarios map[string]*arenaScenario `json:"loaded_scenarios,omitempty"`
}

// ExtractToolInfo parses an ArenaConfig JSON string and returns tool info
// for deploy planning. It reads from both inline ToolSpecs and loaded tool data.
func ExtractToolInfo(arenaConfigJSON string) ([]ToolInfo, error) {
	if arenaConfigJSON == "" {
		return nil, nil
	}
	var cfg arenaConfig
	if err := json.Unmarshal([]byte(arenaConfigJSON), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse arena config: %w", err)
	}
	var tools []ToolInfo

	// From ToolSpecs (inline specs — already structured)
	for name, spec := range cfg.ToolSpecs {
		tools = append(tools, ToolInfo{
			Name:        name,
			Description: spec.Description,
			Mode:        spec.Mode,
			HasSchema:   spec.InputSchema != nil,
			InputSchema: spec.InputSchema,
			HTTPURL:     httpURL(spec.HTTP),
			HTTPMethod:  httpMethod(spec.HTTP),
		})
	}

	// From LoadedTools (file-ref tools — need YAML parsing from Data bytes)
	for _, td := range cfg.LoadedTools {
		info, err := parseToolData(td)
		if err != nil {
			continue
		}
		if !containsTool(tools, info.Name) {
			tools = append(tools, info)
		}
	}

	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

// ExtractToolPolicies returns a merged ToolPolicyInfo from all scenarios in the ArenaConfig.
func ExtractToolPolicies(arenaConfigJSON string) (*ToolPolicyInfo, error) {
	if arenaConfigJSON == "" {
		return nil, nil
	}
	var cfg arenaConfigWithScenarios
	if err := json.Unmarshal([]byte(arenaConfigJSON), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse arena config for policies: %w", err)
	}
	seen := map[string]bool{}
	var blocklist []string
	for _, sc := range cfg.LoadedScenarios {
		if sc.ToolPolicy == nil {
			continue
		}
		for _, b := range sc.ToolPolicy.Blocklist {
			if !seen[b] {
				seen[b] = true
				blocklist = append(blocklist, b)
			}
		}
	}
	sort.Strings(blocklist)
	if len(blocklist) == 0 {
		return nil, nil
	}
	return &ToolPolicyInfo{Blocklist: blocklist}, nil
}

func parseToolData(td arenaToolData) (ToolInfo, error) {
	if len(td.Data) == 0 {
		return ToolInfo{}, fmt.Errorf("no data in tool entry")
	}
	var manifest toolManifest
	if err := yaml.Unmarshal(td.Data, &manifest); err != nil {
		return ToolInfo{}, fmt.Errorf("failed to parse tool manifest YAML: %w", err)
	}
	name := manifest.Spec.Name
	if name == "" {
		name = manifest.Metadata.Name
	}
	if name == "" {
		return ToolInfo{}, fmt.Errorf("tool manifest has no name")
	}
	return ToolInfo{
		Name:        name,
		Description: manifest.Spec.Description,
		Mode:        manifest.Spec.Mode,
		HasSchema:   manifest.Spec.InputSchema != nil,
		InputSchema: manifest.Spec.InputSchema,
		HTTPURL:     httpURL(manifest.Spec.HTTP),
		HTTPMethod:  httpMethod(manifest.Spec.HTTP),
	}, nil
}

func httpURL(h *arenaHTTP) string {
	if h == nil {
		return ""
	}
	return h.URL
}

func httpMethod(h *arenaHTTP) string {
	if h == nil {
		return ""
	}
	return h.Method
}

func containsTool(tools []ToolInfo, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}
