package deploy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The Links field is optional end to end. These tests pin the two properties
// the protocol depends on: an adapter that sets no links produces JSON with no
// `links` key at all (so old adapters and new clients interoperate), and links
// that ARE set survive a marshal/unmarshal round trip intact.

func TestResourceResult_LinksOmittedWhenAbsent(t *testing.T) {
	b, err := json.Marshal(ResourceResult{
		Type: "agent_runtime", Name: "a", Action: ActionCreate, Status: "created",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "links") {
		t.Errorf("a result with no links must not emit the key at all, got %s", b)
	}
}

func TestResourceStatus_LinksOmittedWhenAbsent(t *testing.T) {
	b, err := json.Marshal(ResourceStatus{Type: "agent_runtime", Name: "a", Status: "healthy"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "links") {
		t.Errorf("a status with no links must not emit the key at all, got %s", b)
	}
}

func TestStatusResponse_LinksOmittedWhenAbsent(t *testing.T) {
	b, err := json.Marshal(StatusResponse{Status: "deployed"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "links") {
		t.Errorf("a response with no links must not emit the key at all, got %s", b)
	}
}

func TestResourceResult_LinksRoundTrip(t *testing.T) {
	in := ResourceResult{
		Type: "agent_runtime", Name: "a", Action: ActionCreate, Status: "created",
		Links: []ResourceLink{
			{Label: "Console", URL: "https://omnia.example.com/agents/a?workspace=ws", Rel: "console"},
			{Label: "Logs", URL: "https://omnia.example.com/agents/a/logs"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out ResourceResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Links) != 2 {
		t.Fatalf("links = %+v, want 2", out.Links)
	}
	if out.Links[0] != in.Links[0] {
		t.Errorf("links[0] = %+v, want %+v", out.Links[0], in.Links[0])
	}
	// Rel is optional and must not be invented on the way through.
	if out.Links[1].Rel != "" {
		t.Errorf("links[1].Rel = %q, want it left empty", out.Links[1].Rel)
	}
}

// TestResourceLink_UnknownFieldIsIgnored covers the forward-compatible
// direction: a NEWER adapter that adds a field to a link must not break an
// older client decoding it.
func TestResourceLink_UnknownFieldIsIgnored(t *testing.T) {
	var out ResourceResult
	if err := json.Unmarshal([]byte(`{
		"type":"agent_runtime","name":"a","action":"CREATE","status":"created",
		"links":[{"label":"Console","url":"https://example.com","rel":"console","icon":"rocket"}]
	}`), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Links) != 1 || out.Links[0].Label != "Console" {
		t.Fatalf("links = %+v, want the link decoded despite the unknown field", out.Links)
	}
}

// TestLinksAbsentDecodesToNil pins the client-side half of "absent means no
// links": a payload from an OLDER adapter, with no links key, decodes to nil
// rather than an empty non-nil slice, so `len(...) == 0` and `== nil` agree.
func TestLinksAbsentDecodesToNil(t *testing.T) {
	var out ResourceResult
	if err := json.Unmarshal(
		[]byte(`{"type":"agent_runtime","name":"a","action":"CREATE","status":"created"}`), &out,
	); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Links != nil {
		t.Errorf("Links = %+v, want nil for a payload with no links key", out.Links)
	}
}

// ---------------------------------------------------------------------------
// JSON-RPC round trip
// ---------------------------------------------------------------------------

// linksHandler is a mock adapter that returns links on both an Apply resource
// event and a Status resource, plus a deployment-wide link on the response.
func linksHandler(method string, _ json.RawMessage) (any, *rpcError) {
	switch method {
	case "status":
		return StatusResponse{
			Status: "deployed",
			Resources: []ResourceStatus{{
				Type: "agent_runtime", Name: "a", Status: "healthy",
				Links: []ResourceLink{{
					Label: "Console", URL: "https://omnia.example.com/agents/a?workspace=ws",
					Rel: "console",
				}},
			}},
			Links: []ResourceLink{{Label: "Workspace", URL: "https://omnia.example.com/ws"}},
		}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

// TestClientStatus_LinksSurviveTheWire is the round trip the protocol actually
// depends on: links set by an adapter must reach a client through the JSON-RPC
// transport, not merely survive struct marshalling.
func TestClientStatus_LinksSurviveTheWire(t *testing.T) {
	client := startTestClient(t, linksHandler)

	status, err := client.Status(context.Background(), &StatusRequest{DeployConfig: `{}`})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if len(status.Resources) != 1 {
		t.Fatalf("resources = %+v, want 1", status.Resources)
	}
	links := status.Resources[0].Links
	if len(links) != 1 {
		t.Fatalf("resource links = %+v, want 1", links)
	}
	if links[0].URL != "https://omnia.example.com/agents/a?workspace=ws" {
		t.Errorf("resource link URL = %q", links[0].URL)
	}
	if links[0].Rel != "console" {
		t.Errorf("resource link Rel = %q, want console", links[0].Rel)
	}

	if len(status.Links) != 1 || status.Links[0].Label != "Workspace" {
		t.Errorf("deployment-wide links = %+v, want the Workspace link", status.Links)
	}
}

// TestClientStatus_NoLinksFromOlderAdapter covers the compatibility direction
// that matters most: an adapter that knows nothing about links (defaultHandler
// predates the field) must decode cleanly with none, not error.
func TestClientStatus_NoLinksFromOlderAdapter(t *testing.T) {
	client := startTestClient(t, defaultHandler)

	status, err := client.Status(context.Background(), &StatusRequest{DeployConfig: `{}`})
	if err != nil {
		t.Fatalf("Status against a links-unaware adapter: %v", err)
	}
	if status.Links != nil {
		t.Errorf("deployment links = %+v, want nil", status.Links)
	}
	for i, r := range status.Resources {
		if r.Links != nil {
			t.Errorf("resources[%d].Links = %+v, want nil", i, r.Links)
		}
	}
}
