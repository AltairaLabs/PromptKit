package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Vertex serves a regional location on {region}-aiplatform, the global
// location on the bare host, and the us/eu multi-regions on their .rep.
// hosts; "{location}-aiplatform" does not resolve for global, us or eu.
func TestVertexEndpoints_UseTheLocationsHost(t *testing.T) {
	cases := []struct{ location, host string }{
		{"us-central1", "https://us-central1-aiplatform.googleapis.com"},
		{"global", "https://aiplatform.googleapis.com"},
		{"us", "https://aiplatform.us.rep.googleapis.com"},
		{"eu", "https://aiplatform.eu.rep.googleapis.com"},
	}
	for _, tc := range cases {
		t.Run(tc.location, func(t *testing.T) {
			path := "/projects/p/locations/" + tc.location
			assert.Equal(t, tc.host+"/v1"+path+"/publishers/anthropic/models", VertexEndpoint("p", tc.location))
			assert.Equal(t, tc.host+"/v1"+path+"/publishers/google/models", VertexEmbeddingEndpoint("p", tc.location))
			assert.Equal(t, tc.host+"/v1beta1"+path+"/endpoints/openapi", VertexOpenAIEndpoint("p", tc.location, ""))
		})
	}
}

// A location's case and surrounding space do not change its host.
func TestVertexHost_NormalizesTheLocation(t *testing.T) {
	assert.Equal(t, "https://aiplatform.googleapis.com", VertexHost("Global"))
	assert.Equal(t, "https://aiplatform.eu.rep.googleapis.com", VertexHost(" EU "))
	assert.Equal(t, "https://us-central1-aiplatform.googleapis.com", VertexHost("US-Central1"))
}

// The location base, the Live URL and a Google model's resource name share
// one host and path layout with the publisher endpoints.
func TestVertexLocationHelpers(t *testing.T) {
	assert.Equal(t, "https://aiplatform.us.rep.googleapis.com/v1/projects/p/locations/us", VertexLocationBase("p", "us"))
	assert.Equal(t,
		"wss://aiplatform.us.rep.googleapis.com/ws/google.cloud.aiplatform.v1.LlmBidiService/BidiGenerateContent",
		VertexLiveURL("us"))
	assert.Equal(t, "projects/p/locations/us-central1/publishers/google/models/gemini-3.8-live",
		VertexGoogleModelResource("p", "us-central1", "gemini-3.8-live"))
}
