package a2a

import "encoding/json"

// SecurityScheme declares one way a caller can authenticate (A2A 1.0 §4.5).
// Exactly one field is set. It marshals in the 1.0 shape
// ({"httpAuthSecurityScheme": {...}}) and unmarshals from either that or 0.3's
// OpenAPI-style {"type": "http", ...}.
type SecurityScheme struct {
	APIKey        *APIKeySecurityScheme        `json:"apiKeySecurityScheme,omitempty"`
	HTTPAuth      *HTTPAuthSecurityScheme      `json:"httpAuthSecurityScheme,omitempty"`
	OAuth2        *OAuth2SecurityScheme        `json:"oauth2SecurityScheme,omitempty"`
	OpenIDConnect *OpenIDConnectSecurityScheme `json:"openIdConnectSecurityScheme,omitempty"`
	MutualTLS     *MutualTLSSecurityScheme     `json:"mtlsSecurityScheme,omitempty"`
}

// APIKeySecurityScheme is an API key sent in a header, query parameter or
// cookie.
type APIKeySecurityScheme struct {
	Description string `json:"description,omitempty"`
	// Location is "header", "query" or "cookie".
	Location string `json:"location"`
	Name     string `json:"name"`
}

// HTTPAuthSecurityScheme is an HTTP Authorization scheme such as Bearer.
type HTTPAuthSecurityScheme struct {
	Description  string `json:"description,omitempty"`
	Scheme       string `json:"scheme"`
	BearerFormat string `json:"bearerFormat,omitempty"`
}

// OAuth2SecurityScheme is OAuth 2.0.
type OAuth2SecurityScheme struct {
	Description       string     `json:"description,omitempty"`
	Flows             OAuthFlows `json:"flows"`
	OAuth2MetadataURL string     `json:"oauth2MetadataUrl,omitempty"`
}

// OAuthFlows lists the OAuth 2.0 flows the agent supports.
type OAuthFlows struct {
	AuthorizationCode *OAuthFlow `json:"authorizationCode,omitempty"`
	ClientCredentials *OAuthFlow `json:"clientCredentials,omitempty"`
	DeviceCode        *OAuthFlow `json:"deviceCode,omitempty"`
	Implicit          *OAuthFlow `json:"implicit,omitempty"`
	Password          *OAuthFlow `json:"password,omitempty"`
}

// OAuthFlow is one OAuth 2.0 flow; which URLs apply depends on the flow.
type OAuthFlow struct {
	AuthorizationURL       string            `json:"authorizationUrl,omitempty"`
	DeviceAuthorizationURL string            `json:"deviceAuthorizationUrl,omitempty"`
	TokenURL               string            `json:"tokenUrl,omitempty"`
	RefreshURL             string            `json:"refreshUrl,omitempty"`
	Scopes                 map[string]string `json:"scopes"`
	PKCERequired           bool              `json:"pkceRequired,omitempty"`
}

// OpenIDConnectSecurityScheme is OpenID Connect discovery.
type OpenIDConnectSecurityScheme struct {
	Description      string `json:"description,omitempty"`
	OpenIDConnectURL string `json:"openIdConnectUrl"`
}

// MutualTLSSecurityScheme is mutual TLS.
type MutualTLSSecurityScheme struct {
	Description string `json:"description,omitempty"`
}

// SecurityRequirement names the schemes (and their scopes) a caller must
// satisfy together. It marshals in the 1.0 shape
// ({"schemes": {"bearer": {"list": []}}}) and unmarshals from that or 0.3's
// {"bearer": []}.
type SecurityRequirement struct {
	Schemes map[string][]string
}

// RequireScheme returns a requirement for a single scheme with optional
// scopes.
func RequireScheme(name string, scopes ...string) SecurityRequirement {
	if scopes == nil {
		scopes = []string{}
	}
	return SecurityRequirement{Schemes: map[string][]string{name: scopes}}
}

type stringList struct {
	List []string `json:"list"`
}

// MarshalJSON implements json.Marshaler in the A2A 1.0 shape.
func (r SecurityRequirement) MarshalJSON() ([]byte, error) {
	schemes := make(map[string]stringList, len(r.Schemes))
	for name, scopes := range r.Schemes {
		if scopes == nil {
			scopes = []string{}
		}
		schemes[name] = stringList{List: scopes}
	}
	return json.Marshal(struct {
		Schemes map[string]stringList `json:"schemes"`
	}{schemes})
}

// UnmarshalJSON implements json.Unmarshaler for both versions' shapes.
func (r *SecurityRequirement) UnmarshalJSON(data []byte) error {
	var v1 struct {
		Schemes map[string]stringList `json:"schemes"`
	}
	if err := json.Unmarshal(data, &v1); err == nil && v1.Schemes != nil {
		r.Schemes = make(map[string][]string, len(v1.Schemes))
		for name, l := range v1.Schemes {
			r.Schemes[name] = l.List
		}
		return nil
	}
	var v03 map[string][]string
	if err := json.Unmarshal(data, &v03); err != nil {
		return err
	}
	r.Schemes = v03
	return nil
}

// 0.3 security scheme types.
const (
	schemeTypeAPIKey        = "apiKey"
	schemeTypeHTTP          = "http"
	schemeTypeOAuth2        = "oauth2"
	schemeTypeOpenIDConnect = "openIdConnect"
	schemeTypeMutualTLS     = "mutualTLS"
)

// v03SecurityScheme is 0.3's flat, OpenAPI-style scheme.
type v03SecurityScheme struct {
	Type              string      `json:"type"`
	Description       string      `json:"description,omitempty"`
	In                string      `json:"in,omitempty"`
	Name              string      `json:"name,omitempty"`
	Scheme            string      `json:"scheme,omitempty"`
	BearerFormat      string      `json:"bearerFormat,omitempty"`
	Flows             *OAuthFlows `json:"flows,omitempty"`
	OAuth2MetadataURL string      `json:"oauth2MetadataUrl,omitempty"`
	OpenIDConnectURL  string      `json:"openIdConnectUrl,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler for both versions' shapes.
func (s *SecurityScheme) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Type == "" {
		type plain SecurityScheme
		return json.Unmarshal(data, (*plain)(s))
	}
	var v v03SecurityScheme
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*s = SecurityScheme{}
	switch v.Type {
	case schemeTypeAPIKey:
		s.APIKey = &APIKeySecurityScheme{Description: v.Description, Location: v.In, Name: v.Name}
	case schemeTypeHTTP:
		s.HTTPAuth = &HTTPAuthSecurityScheme{
			Description: v.Description, Scheme: v.Scheme, BearerFormat: v.BearerFormat,
		}
	case schemeTypeOAuth2:
		o := &OAuth2SecurityScheme{Description: v.Description, OAuth2MetadataURL: v.OAuth2MetadataURL}
		if v.Flows != nil {
			o.Flows = *v.Flows
		}
		s.OAuth2 = o
	case schemeTypeOpenIDConnect:
		s.OpenIDConnect = &OpenIDConnectSecurityScheme{
			Description: v.Description, OpenIDConnectURL: v.OpenIDConnectURL,
		}
	case schemeTypeMutualTLS:
		s.MutualTLS = &MutualTLSSecurityScheme{Description: v.Description}
	}
	return nil
}

// v03 returns the scheme in 0.3's shape.
func (s *SecurityScheme) v03() v03SecurityScheme {
	switch {
	case s.APIKey != nil:
		return v03SecurityScheme{Type: schemeTypeAPIKey, Description: s.APIKey.Description,
			In: s.APIKey.Location, Name: s.APIKey.Name}
	case s.HTTPAuth != nil:
		return v03SecurityScheme{Type: schemeTypeHTTP, Description: s.HTTPAuth.Description,
			Scheme: s.HTTPAuth.Scheme, BearerFormat: s.HTTPAuth.BearerFormat}
	case s.OAuth2 != nil:
		flows := s.OAuth2.Flows
		return v03SecurityScheme{Type: schemeTypeOAuth2, Description: s.OAuth2.Description,
			Flows: &flows, OAuth2MetadataURL: s.OAuth2.OAuth2MetadataURL}
	case s.OpenIDConnect != nil:
		return v03SecurityScheme{Type: schemeTypeOpenIDConnect, Description: s.OpenIDConnect.Description,
			OpenIDConnectURL: s.OpenIDConnect.OpenIDConnectURL}
	case s.MutualTLS != nil:
		return v03SecurityScheme{Type: schemeTypeMutualTLS, Description: s.MutualTLS.Description}
	default:
		return v03SecurityScheme{}
	}
}
