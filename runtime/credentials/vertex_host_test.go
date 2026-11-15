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
