package a2a

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Agent cards round-trip (#2131): a card decoded from a peer and encoded again
// keeps every field the A2A spec defines, and an encoded card always carries
// the fields a2a.proto marks REQUIRED, even when they are empty.

func decodeCardFixture(t *testing.T, path string) AgentCard {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var card AgentCard
	require.NoError(t, json.Unmarshal(data, &card))
	return card
}

func TestAgentCard_V1FixtureRoundTripsExactly(t *testing.T) {
	want, err := os.ReadFile("testdata/agent_card_v1.json")
	require.NoError(t, err)
	card := decodeCardFixture(t, "testdata/agent_card_v1.json")

	got, err := json.Marshal(ProtocolVersion10.WireAgentCard(&card))
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

func TestAgentCard_V03FixtureKeepsSignaturesParamsAndSkillSecurity(t *testing.T) {
	card := decodeCardFixture(t, "testdata/agent_card_v03.json")

	v1, err := json.Marshal(ProtocolVersion10.WireAgentCard(&card))
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"name": "Recipe Agent",
		"description": "Agent that helps users with recipes and cooking.",
		"supportedInterfaces": [
			{"url": "https://recipes.example.com/a2a", "protocolBinding": "JSONRPC", "protocolVersion": "0.3"}
		],
		"provider": {"organization": "Example Kitchens", "url": "https://kitchens.example.com"},
		"version": "0.9.1",
		"capabilities": {
			"streaming": true,
			"extensions": [{"uri": "https://example.com/ext/units/v1", "params": {"system": "metric"}}]
		},
		"securitySchemes": {
			"oidc": {"openIdConnectSecurityScheme": {
				"openIdConnectUrl": "https://id.example.com/.well-known/openid-configuration"}}
		},
		"securityRequirements": [{"schemes": {"oidc": {"list": ["openid"]}}}],
		"defaultInputModes": ["text/plain"],
		"defaultOutputModes": ["text/plain"],
		"skills": [{
			"id": "find-recipe", "name": "Find recipe", "description": "Finds recipes by ingredient.",
			"tags": ["cooking"],
			"securityRequirements": [{"schemes": {"oidc": {"list": ["recipes.read"]}}}]
		}],
		"signatures": [{"protected": "eyJhbGciOiJFUzI1NiJ9", "signature": "c2lnbmF0dXJl", "header": {"kid": "k2"}}]
	}`, string(v1))

	// Served back to a 0.3 peer, skill security and extension params keep
	// their 0.3 shape. Signatures are not re-sent: they sign the card the
	// signer published, which the 0.3 rendering (with supportedInterfaces
	// added) is not.
	v03, err := json.Marshal(ProtocolVersion03.WireAgentCard(&card))
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"protocolVersion": "0.3.0",
		"name": "Recipe Agent",
		"description": "Agent that helps users with recipes and cooking.",
		"url": "https://recipes.example.com/a2a",
		"preferredTransport": "JSONRPC",
		"additionalInterfaces": [{"url": "https://recipes.example.com/a2a", "transport": "JSONRPC"}],
		"supportedInterfaces": [
			{"url": "https://recipes.example.com/a2a", "protocolBinding": "JSONRPC", "protocolVersion": "0.3"}
		],
		"provider": {"organization": "Example Kitchens", "url": "https://kitchens.example.com"},
		"version": "0.9.1",
		"capabilities": {
			"streaming": true,
			"extensions": [{"uri": "https://example.com/ext/units/v1", "params": {"system": "metric"}}]
		},
		"securitySchemes": {
			"oidc": {"type": "openIdConnect",
				"openIdConnectUrl": "https://id.example.com/.well-known/openid-configuration"}
		},
		"security": [{"oidc": ["openid"]}],
		"defaultInputModes": ["text/plain"],
		"defaultOutputModes": ["text/plain"],
		"skills": [{
			"id": "find-recipe", "name": "Find recipe", "description": "Finds recipes by ingredient.",
			"tags": ["cooking"],
			"security": [{"oidc": ["recipes.read"]}]
		}]
	}`, string(v03))
}

func TestAgentCard_EncodesRequiredFieldsWhenEmpty(t *testing.T) {
	card := AgentCard{
		Name:                "bare",
		Provider:            &AgentProvider{Organization: "org"},
		SupportedInterfaces: []AgentInterface{{URL: "https://a.example/a2a"}},
		Skills:              []AgentSkill{{ID: "s", Name: "S"}},
	}
	got, err := json.Marshal(card)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"name": "bare",
		"description": "",
		"supportedInterfaces": [{"url": "https://a.example/a2a", "protocolBinding": "", "protocolVersion": ""}],
		"provider": {"organization": "org", "url": ""},
		"version": "",
		"capabilities": {},
		"defaultInputModes": [],
		"defaultOutputModes": [],
		"skills": [{"id": "s", "name": "S", "description": "", "tags": []}]
	}`, string(got))

	// A pointer encodes the same as the value.
	viaPtr, err := json.Marshal(&card)
	require.NoError(t, err)
	assert.JSONEq(t, string(got), string(viaPtr))

	// The zero card still names every REQUIRED field.
	zero, err := json.Marshal(AgentCard{})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"name": "", "description": "", "supportedInterfaces": [], "version": "",
		"capabilities": {}, "defaultInputModes": [], "defaultOutputModes": [], "skills": []
	}`, string(zero))
}
