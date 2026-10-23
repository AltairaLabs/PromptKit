package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	// kindEval is the K8s-style kind value for Eval configurations
	kindEval = "Eval"

	// schemaKey is the top-level key editors read to find a file's JSON
	// schema. It is not part of any manifest type, so the strict decode drops it.
	schemaKey = "$schema"

	// fieldAPIVersion and fieldKind are the manifest keys naming the config
	// format version and the resource type.
	fieldAPIVersion = "apiVersion"
	fieldKind       = "kind"
)

// manifestRequiredKeys are the top-level keys every K8s-style manifest carries.
var manifestRequiredKeys = []string{fieldAPIVersion, fieldKind, "spec"}

// K8sManifest is an interface for K8s-style manifest types.
type K8sManifest interface {
	GetAPIVersion() string
	GetKind() string
	GetName() string
	SetID(id string)
}

// LoadSimpleK8sManifest is a generic loader for K8s-style manifest files
// (Provider here; other modules load their own kinds through it).
//
// The file is checked against T itself: apiVersion, kind and spec must be
// present, and every key must be a field of T, so a misspelled or unsupported
// setting is an error rather than silently ignored. A top-level $schema key is
// allowed. No JSON schema is consulted — a tool that publishes one for these
// files validates against it itself.
func LoadSimpleK8sManifest[T K8sManifest](filename, expectedKind string) (T, error) {
	var zero T

	data, err := os.ReadFile(filename)
	if err != nil {
		return zero, fmt.Errorf("failed to read %s file: %w", expectedKind, err)
	}

	config, err := decodeManifest[T](data)
	if err != nil {
		return zero, fmt.Errorf("invalid %s file %s: %w", expectedKind, filename, err)
	}

	// metadata.name is the resource name; spec.id is the identifier other
	// config refers to. metadata.name is only the fallback for an absent id.
	if explicitID(config) == "" {
		config.SetID(config.GetName())
	}
	return config, nil
}

// decodeManifest strictly decodes a K8s-style manifest into T.
func decodeManifest[T any](data []byte) (T, error) {
	var zero T

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return zero, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return zero, errors.New("a manifest must be a YAML mapping")
	}
	root := doc.Content[0]

	present := map[string]bool{}
	kept := root.Content[:0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if key == schemaKey {
			continue
		}
		present[key] = true
		kept = append(kept, root.Content[i], root.Content[i+1])
	}
	root.Content = kept
	for _, key := range manifestRequiredKeys {
		if !present[key] {
			return zero, fmt.Errorf("missing required field %q", key)
		}
	}

	// yaml.Node.Decode has no strict mode, so re-encode and decode with one.
	stripped, err := yaml.Marshal(root)
	if err != nil {
		return zero, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(stripped))
	dec.KnownFields(true)
	var out T
	if err := dec.Decode(&out); err != nil {
		return zero, err
	}
	return out, nil
}

// K8sManifestWithID is implemented by manifests whose spec carries an explicit
// id. LoadSimpleK8sManifest keeps that id rather than overwriting it with
// metadata.name. It is separate from K8sManifest so existing implementations
// keep compiling.
type K8sManifestWithID interface {
	GetID() string
}

func explicitID(m any) string {
	if withID, ok := m.(K8sManifestWithID); ok {
		return withID.GetID()
	}
	return ""
}

// LoadProvider loads and parses a provider configuration from a YAML file in
// K8s-style manifest format. Beyond the strict decode, spec.type is required
// and spec.role must be a known role.
func LoadProvider(filename string) (*Provider, error) {
	config, err := LoadSimpleK8sManifest[*ProviderConfigK8s](filename, "Provider")
	if err != nil {
		return nil, err
	}
	if config.Spec.Type == "" {
		return nil, fmt.Errorf("invalid Provider file %s: spec.type is required", filename)
	}
	if err := config.Spec.ValidateRole(); err != nil {
		return nil, fmt.Errorf("invalid Provider file %s: %w", filename, err)
	}
	return &config.Spec, nil
}
