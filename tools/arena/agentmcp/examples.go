package agentmcp

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

// exampleLoader is the subset of templates.Loader that renderExample needs.
type exampleLoader interface {
	Load(name string) (*templates.Template, error)
	ReadTemplateFile(templateName, filePath string) ([]byte, error)
}

// renderExample resolves a catalog entry by name and renders its files as text.
// Builtin sources resolve offline via the templates loader.
func renderExample(loader exampleLoader, name string) (string, error) {
	cat, err := agentkb.LoadCatalog()
	if err != nil {
		return "", err
	}

	var entry *agentkb.CatalogEntry
	for i := range cat.Entries {
		if cat.Entries[i].Name == name {
			entry = &cat.Entries[i]
			break
		}
	}
	if entry == nil {
		return "", fmt.Errorf("unknown example %q (see list_examples)", name)
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

func exampleFileBody(loader exampleLoader, templateName string, f *templates.FileSpec) string {
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
