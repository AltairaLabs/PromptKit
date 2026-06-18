package agentmcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
)

const (
	uriConceptPrefix = "promptarena://concepts/"
	uriSchemaPrefix  = "promptarena://schemas/"
	uriCatalog       = "promptarena://catalog"

	mimeMarkdown = "text/markdown"
	mimeJSON     = "application/json"
)

// resourceDef describes one MCP resource in resources/list.
type resourceDef struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type resourcesListResult struct {
	Resources []resourceDef `json:"resources"`
}

// resourceContents is one returned body in resources/read.
type resourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text"`
}

type resourceReadResult struct {
	Contents []resourceContents `json:"contents"`
}

func (s *Server) resourcesList() resourcesListResult {
	var res []resourceDef

	if concepts, err := agentkb.Concepts(); err == nil {
		for _, c := range concepts {
			res = append(res, resourceDef{
				URI:         uriConceptPrefix + c.ID,
				Name:        c.Title,
				Description: c.Summary,
				MimeType:    mimeMarkdown,
			})
		}
	}

	if names, err := agentkb.SchemaNames(); err == nil {
		for _, n := range names {
			res = append(res, resourceDef{
				URI:      uriSchemaPrefix + n,
				Name:     n + " schema",
				MimeType: mimeJSON,
			})
		}
	}

	res = append(res, resourceDef{
		URI:         uriCatalog,
		Name:        "example catalog",
		Description: "Index of example kits.",
		MimeType:    mimeJSON,
	})

	return resourcesListResult{Resources: res}
}

func (s *Server) handleResourceRead(msg *mcp.JSONRPCMessage) *mcp.JSONRPCMessage {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return s.errorResp(msg.ID, invalidParams, "invalid params: "+err.Error())
	}

	text, mime, err := readResourceBody(p.URI)
	if err != nil {
		return s.errorResp(msg.ID, invalidParams, err.Error())
	}
	return s.result(msg.ID, resourceReadResult{
		Contents: []resourceContents{{URI: p.URI, MimeType: mime, Text: text}},
	})
}

func readResourceBody(uri string) (text, mime string, err error) {
	switch {
	case uri == uriCatalog:
		cat, lErr := agentkb.LoadCatalog()
		if lErr != nil {
			return "", "", lErr
		}
		raw, mErr := json.MarshalIndent(cat, "", "  ")
		if mErr != nil {
			return "", "", mErr
		}
		return string(raw), mimeJSON, nil

	case strings.HasPrefix(uri, uriConceptPrefix):
		c, cErr := agentkb.ConceptByID(strings.TrimPrefix(uri, uriConceptPrefix))
		if cErr != nil {
			return "", "", cErr
		}
		return "# " + c.Title + "\n\n" + c.Body, mimeMarkdown, nil

	case strings.HasPrefix(uri, uriSchemaPrefix):
		b, sErr := agentkb.Schema(strings.TrimPrefix(uri, uriSchemaPrefix))
		if sErr != nil {
			return "", "", sErr
		}
		return string(b), mimeJSON, nil

	default:
		return "", "", fmt.Errorf("unknown resource uri: %s", uri)
	}
}
