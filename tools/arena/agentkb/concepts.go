// Package agentkb is the embedded knowledge base that briefs AI coding agents
// building PromptArena kits. Concepts are the single hand-authored source;
// the skill, schema, and catalog accessors are layered on top.
package agentkb

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed concepts/*.md
var conceptsFS embed.FS

// Concept is one authoring idiom parsed from concepts/<id>.md.
type Concept struct {
	ID      string
	Title   string
	Summary string
	Tags    []string
	Body    string
}

type conceptFrontmatter struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Summary string   `yaml:"summary"`
	Tags    []string `yaml:"tags"`
}

const frontmatterFence = "---\n"

func parseConcept(name string, raw []byte) (Concept, error) {
	s := string(raw)
	if !strings.HasPrefix(s, frontmatterFence) {
		return Concept{}, fmt.Errorf("concept %s: missing frontmatter", name)
	}
	rest := s[len(frontmatterFence):]
	fmText, rawBody, found := strings.Cut(rest, "\n"+frontmatterFence)
	if !found {
		return Concept{}, fmt.Errorf("concept %s: unterminated frontmatter", name)
	}
	var fm conceptFrontmatter
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return Concept{}, fmt.Errorf("concept %s: parse frontmatter: %w", name, err)
	}
	if fm.ID == "" || fm.Title == "" || fm.Summary == "" {
		return Concept{}, fmt.Errorf("concept %s: id, title and summary are required", name)
	}
	body := strings.TrimLeft(rawBody, "\n")
	return Concept{ID: fm.ID, Title: fm.Title, Summary: fm.Summary, Tags: fm.Tags, Body: body}, nil
}

// Concepts returns every embedded concept, sorted by ID.
func Concepts() ([]Concept, error) {
	entries, err := conceptsFS.ReadDir("concepts")
	if err != nil {
		return nil, fmt.Errorf("read concepts dir: %w", err)
	}
	out := make([]Concept, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := conceptsFS.ReadFile("concepts/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read concept %s: %w", e.Name(), err)
		}
		c, err := parseConcept(e.Name(), raw)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ConceptByID returns a single concept by ID.
func ConceptByID(id string) (Concept, error) {
	cs, err := Concepts()
	if err != nil {
		return Concept{}, err
	}
	for _, c := range cs {
		if c.ID == id {
			return c, nil
		}
	}
	return Concept{}, fmt.Errorf("unknown concept %q", id)
}
