package agentkb

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

// ExampleLoader is the subset of templates.Loader that RenderExample needs.
// templates.Loader satisfies it.
type ExampleLoader interface {
	Load(name string) (*templates.Template, error)
	ReadTemplateFile(templateName, filePath string) ([]byte, error)
}

// RenderExample resolves a catalog entry by name and renders its files as text.
// Builtin sources resolve offline via the loader. It is the single definition
// shared by the `examples show` command and the MCP show_example tool.
func RenderExample(loader ExampleLoader, name string) (string, error) {
	cat, err := LoadCatalog()
	if err != nil {
		return "", err
	}

	var entry *CatalogEntry
	for i := range cat.Entries {
		if cat.Entries[i].Name == name {
			entry = &cat.Entries[i]
			break
		}
	}
	if entry == nil {
		return "", fmt.Errorf("unknown example %q (see the example catalog)", name)
	}

	loadName := entry.Source
	if idx := strings.IndexByte(loadName, ':'); idx >= 0 {
		loadName = loadName[idx+1:]
	}

	tmpl, err := loader.Load(loadName)
	if err != nil {
		return "", fmt.Errorf("load example %q: %w", name, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\nFiles:\n", entry.Name, entry.Description)
	for i := range tmpl.Spec.Files {
		f := &tmpl.Spec.Files[i]
		fmt.Fprintf(&b, "\n--- %s ---\n", f.Path)
		b.WriteString(exampleFileBody(loader, tmpl.Metadata.Name, f))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// exampleFileBody returns the displayable body for a template file: inline
// content, the embedded template source, or a placeholder when the body lives
// elsewhere (external/remote source).
func exampleFileBody(loader ExampleLoader, templateName string, f *templates.FileSpec) string {
	switch {
	case f.Content != "":
		return f.Content
	case f.Template != "":
		data, err := loader.ReadTemplateFile(templateName, f.Template)
		if err != nil {
			return fmt.Sprintf("(template body unavailable: %v)", err)
		}
		return string(data)
	default:
		return "(external source — fetch the example to see this file)"
	}
}
