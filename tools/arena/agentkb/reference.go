package agentkb

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed reference/*.md
var referenceFS embed.FS

// ReferenceNames lists the bundled reference doc filenames (sorted).
func ReferenceNames() ([]string, error) {
	entries, err := referenceFS.ReadDir("reference")
	if err != nil {
		return nil, fmt.Errorf("read reference dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Reference returns the bytes of one bundled reference doc by filename.
func Reference(name string) ([]byte, error) {
	b, err := referenceFS.ReadFile("reference/" + name)
	if err != nil {
		return nil, fmt.Errorf("unknown reference %q: %w", name, err)
	}
	return b, nil
}
