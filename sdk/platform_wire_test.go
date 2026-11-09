package sdk

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
)

// convCredServer is a stand-in for a cloud's credential endpoint (STS, OAuth2
// token, managed identity): it records each request and answers with body.
type convCredServer struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []convCredRequest
}

type convCredRequest struct {
	Method, Path string
	Query        url.Values
	Header       http.Header
	Form         url.Values
}

func newConvCredServer(t *testing.T, contentType, body string) *convCredServer {
	t.Helper()
	s := &convCredServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		s.mu.Lock()
		s.reqs = append(s.reqs, convCredRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(),
			Header: r.Header.Clone(), Form: form})
		s.mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *convCredServer) requests() []convCredRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]convCredRequest(nil), s.reqs...)
}

// convIsolateAWS points the AWS SDK at empty config files and the given keys,
// so nothing on the machine running the test is read.
func convIsolateAWS(t *testing.T, keyID string) string {
	t.Helper()
	dir := t.TempDir()
	for k, v := range map[string]string{
		"AWS_ACCESS_KEY_ID": keyID, "AWS_SECRET_ACCESS_KEY": "secret", "AWS_SESSION_TOKEN": "",
		"AWS_PROFILE": "", "AWS_DEFAULT_PROFILE": "", "AWS_REGION": "", "AWS_DEFAULT_REGION": "",
		"AWS_CONFIG_FILE": filepath.Join(dir, "config"), "AWS_SHARED_CREDENTIALS_FILE": filepath.Join(dir, "credentials"),
		"AWS_EC2_METADATA_DISABLED": "true", "AWS_ENDPOINT_URL": "", "AWS_ENDPOINT_URL_STS": "",
	} {
		t.Setenv(k, v)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "credentials"), nil, 0o600))
	return dir
}

// convSendVia opens convChatPack with opts, sends one turn and returns the
// request the endpoint received. The endpoint's 404 answer is not under test.
func convSendVia(t *testing.T, w *convWire, opts ...Option) convWireRequest {
	t.Helper()
	conv, err := Open(createTestPackFile(t, convChatPack), "chat", append([]Option{WithSkipSchemaValidation()}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	_, _ = conv.Send(context.Background(), "hi")
	return w.last(t)
}

// sigV4Credential returns the Credential= scope of a SigV4 Authorization
// header: key ID, date, region, service.
func sigV4Credential(t *testing.T, auth string) []string {
	t.Helper()
	require.True(t, strings.HasPrefix(auth, "AWS4-HMAC-SHA256 "), "not a SigV4 signature: %q", auth)
	i := strings.Index(auth, "Credential=")
	require.GreaterOrEqual(t, i, 0)
	scope := strings.SplitN(auth[i+len("Credential="):], ",", 2)[0]
	return strings.Split(scope, "/")
}

// WithBedrock signs each request with SigV4 for the region and the bedrock
// service, using the credential its options choose, and sends it to the
// model's Bedrock path at WithPlatformEndpoint.
func TestConv_BedrockRequestIsSignedForTheRegion(t *testing.T) {
	model := "anthropic.claude-3-5-haiku-20241022-v1:0"

	t.Run("default chain", func(t *testing.T) {
		convIsolateAWS(t, "AKIDDEFAULT")
		w := newConvWire(t)
		r := convSendVia(t, w, WithBedrock("eu-west-1", "claude", "claude-3-5-haiku-20241022",
			WithPlatformEndpoint(w.URL)))
		assert.Equal(t, "/model/"+model+"/invoke-with-response-stream", r.Path)
		scope := sigV4Credential(t, r.Header.Get("Authorization"))
		assert.Equal(t, "AKIDDEFAULT", scope[0])
		assert.Equal(t, []string{"eu-west-1", "bedrock", "aws4_request"}, scope[2:])
	})

	t.Run("named profile", func(t *testing.T) {
		dir := convIsolateAWS(t, "AKIDDEFAULT")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "credentials"), []byte(
			"[bedrock-prod]\naws_access_key_id = AKIDPROFILE\naws_secret_access_key = s2\n"), 0o600))
		w := newConvWire(t)
		r := convSendVia(t, w, WithBedrock("us-east-1", "claude", "claude-3-5-haiku-20241022",
			WithPlatformEndpoint(w.URL), WithAWSProfile("bedrock-prod")))
		scope := sigV4Credential(t, r.Header.Get("Authorization"))
		assert.Equal(t, "AKIDPROFILE", scope[0], "the profile's key signs, not the environment's")
		assert.Equal(t, "us-east-1", scope[2])
	})

	t.Run("assumed role", func(t *testing.T) {
		convIsolateAWS(t, "AKIDBASE")
		sts := newConvCredServer(t, "text/xml", `<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">`+
			`<AssumeRoleResult><Credentials><AccessKeyId>AKIDROLE</AccessKeyId><SecretAccessKey>s3</SecretAccessKey>`+
			`<SessionToken>ROLETOKEN</SessionToken><Expiration>2099-01-01T00:00:00Z</Expiration></Credentials>`+
			`<AssumedRoleUser><Arn>arn:aws:sts::123456789012:assumed-role/BedrockAccess/s</Arn>`+
			`<AssumedRoleId>AROA:s</AssumedRoleId></AssumedRoleUser></AssumeRoleResult>`+
			`<ResponseMetadata><RequestId>r</RequestId></ResponseMetadata></AssumeRoleResponse>`)
		t.Setenv("AWS_ENDPOINT_URL_STS", sts.URL)
		w := newConvWire(t)
		r := convSendVia(t, w, WithBedrock("us-west-2", "claude", "claude-3-5-haiku-20241022",
			WithPlatformEndpoint(w.URL), WithAWSRoleARN("arn:aws:iam::123456789012:role/BedrockAccess")))

		reqs := sts.requests()
		require.NotEmpty(t, reqs, "the role is assumed through STS")
		assert.Equal(t, "AssumeRole", reqs[0].Form.Get("Action"))
		assert.Equal(t, "arn:aws:iam::123456789012:role/BedrockAccess", reqs[0].Form.Get("RoleArn"))
		assert.Equal(t, "AKIDBASE", sigV4Credential(t, reqs[0].Header.Get("Authorization"))[0],
			"the base credential signs the AssumeRole call")

		scope := sigV4Credential(t, r.Header.Get("Authorization"))
		assert.Equal(t, "AKIDROLE", scope[0], "the role's credential signs the Bedrock call")
		assert.Equal(t, "ROLETOKEN", r.Header.Get("X-Amz-Security-Token"))
	})
}

// A ProviderSpec naming a platform builds a provider that authenticates the
// platform's way: a Bedrock spec signs with SigV4 for its region.
func TestConv_ProviderSpecPlatformSignsTheRequest(t *testing.T) {
	convIsolateAWS(t, "AKIDSPEC")
	w := newConvWire(t)
	r := convSendVia(t, w, WithLLMProvider(ProviderSpec{
		ID: "llm-bedrock", Type: "claude", Model: "anthropic.claude-3-5-haiku-20241022-v1:0", BaseURL: w.URL,
		Platform: &pkgconfig.PlatformConfig{Type: "bedrock", Region: "ap-southeast-2"},
	}))
	assert.Equal(t, "/model/anthropic.claude-3-5-haiku-20241022-v1:0/invoke-with-response-stream", r.Path)
	scope := sigV4Credential(t, r.Header.Get("Authorization"))
	assert.Equal(t, "AKIDSPEC", scope[0])
	assert.Equal(t, []string{"ap-southeast-2", "bedrock", "aws4_request"}, scope[2:])
}

// convServiceAccount writes a GCP service-account key whose token endpoint is
// tokenURL and returns its path.
func convServiceAccount(t *testing.T, tokenURL string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(t, key)})
	sa, err := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "my-proj", "private_key_id": "k1",
		"private_key": string(pemKey), "client_email": "bot@my-proj.iam.gserviceaccount.com",
		"client_id": "1", "token_uri": tokenURL,
	})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "sa.json")
	require.NoError(t, os.WriteFile(path, sa, 0o600))
	return path
}

func mustPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	b, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return b
}

// jwtClaims decodes the claims of a JWT bearer assertion.
func jwtClaims(t *testing.T, assertion string) map[string]any {
	t.Helper()
	parts := strings.Split(assertion, ".")
	require.Len(t, parts, 3, "not a JWT: %q", assertion)
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(raw, &claims))
	return claims
}

// WithVertex authenticates with an OAuth2 token from the service account
// (WithGCPServiceAccount) or Application Default Credentials, and sends the
// request to the model at WithPlatformEndpoint.
func TestConv_VertexRequestCarriesTheServiceAccountToken(t *testing.T) {
	cases := map[string]func(t *testing.T, keyPath string) []PlatformOption{
		"service account key": func(t *testing.T, keyPath string) []PlatformOption {
			t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "absent.json"))
			return []PlatformOption{WithGCPServiceAccount(keyPath)}
		},
		"application default credentials": func(t *testing.T, keyPath string) []PlatformOption {
			t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", keyPath)
			return nil
		},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			tokens := newConvCredServer(t, "application/json",
				`{"access_token":"VERTEXTOKEN","token_type":"Bearer","expires_in":3600}`)
			keyPath := convServiceAccount(t, tokens.URL+"/token")
			w := newConvWire(t)
			r := convSendVia(t, w, WithVertex("europe-west4", "my-proj", "gemini", "gemini-2.0-flash",
				append([]PlatformOption{WithPlatformEndpoint(w.URL)}, opts(t, keyPath)...)...))

			assert.Equal(t, "Bearer VERTEXTOKEN", r.Header.Get("Authorization"))
			assert.Contains(t, r.Path, "/gemini-2.0-flash:")
			reqs := tokens.requests()
			require.NotEmpty(t, reqs, "the token comes from the key's token endpoint")
			claims := jwtClaims(t, reqs[0].Form.Get("assertion"))
			assert.Equal(t, "bot@my-proj.iam.gserviceaccount.com", claims["iss"])
			assert.Equal(t, "https://www.googleapis.com/auth/cloud-platform", claims["scope"])
		})
	}
}

// WithAzureManagedIdentity authenticates the deployment request with a token
// from the managed identity endpoint, asked for with the caller's client ID.
func TestConv_AzureManagedIdentityTokenReachesTheDeployment(t *testing.T) {
	for _, k := range []string{"AZURE_CLIENT_ID", "AZURE_TENANT_ID", "AZURE_CLIENT_SECRET", "MSI_ENDPOINT", "MSI_SECRET",
		"IDENTITY_SERVER_THUMBPRINT", "IMDS_ENDPOINT", "AZURE_FEDERATED_TOKEN_FILE"} {
		t.Setenv(k, "")
	}
	expires := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	msi := newConvCredServer(t, "application/json",
		`{"access_token":"MITOKEN","expires_on":"`+expires+`","resource":"https://cognitiveservices.azure.com","token_type":"Bearer"}`)
	t.Setenv("IDENTITY_ENDPOINT", msi.URL+"/msi/token")
	t.Setenv("IDENTITY_HEADER", "header-secret")

	w := newConvWire(t)
	r := convSendVia(t, w, WithAzure(w.URL, "openai", "gpt-4o", WithAzureManagedIdentity("user-assigned-id")))
	assert.Equal(t, "/openai/deployments/gpt-4o/chat/completions", r.Path)
	assert.NotEmpty(t, r.Query.Get("api-version"))
	assert.Equal(t, "Bearer MITOKEN", r.Header.Get("Authorization"))

	reqs := msi.requests()
	require.NotEmpty(t, reqs)
	assert.Equal(t, "user-assigned-id", reqs[0].Query.Get("client_id"))
	assert.Equal(t, "header-secret", reqs[0].Header.Get("X-Identity-Header"))
	assert.Contains(t, reqs[0].Query.Get("resource"), "cognitiveservices.azure.com")
}

// convEntraHost is the public Entra ID host the identity library talks to.
const convEntraHost = "login.microsoftonline.com"

// convEntraServer is a TLS stand-in for Microsoft Entra ID, serving as
// convEntraHost under a test CA: instance and tenant discovery, then a token
// endpoint that records the client credentials. It returns the server, the
// CA certificate's PEM path for a client to trust, and the recorder.
func convEntraServer(t *testing.T) (*httptest.Server, string, *convCredServer) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "conv test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true,
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	ca, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	leafTmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: convEntraHost},
		DNSNames: []string{convEntraHost}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	require.NoError(t, err)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600))

	rec := &convCredServer{}
	host := "https://" + convEntraHost
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, convCredRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(),
			Header: r.Header.Clone(), Form: form})
		rec.mu.Unlock()
		tenant := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)[0]
		if tenant == "common" {
			tenant = "tenant-1"
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "discovery/instance"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tenant_discovery_endpoint": host + "/" + tenant + "/v2.0/.well-known/openid-configuration",
				"api-version":               "1.1",
				"metadata": []map[string]any{{"preferred_network": convEntraHost,
					"preferred_cache": convEntraHost, "aliases": []string{convEntraHost}}},
			})
		case strings.HasSuffix(r.URL.Path, "openid-configuration"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token_endpoint":         host + "/" + tenant + "/oauth2/v2.0/token",
				"authorization_endpoint": host + "/" + tenant + "/oauth2/v2.0/authorize",
				"issuer":                 host + "/" + tenant + "/v2.0",
			})
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			_, _ = io.WriteString(w, `{"access_token":"CSTOKEN","token_type":"Bearer","expires_in":3600,"ext_expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, caPath, rec
}

// convConnectProxy is an HTTPS proxy that tunnels every CONNECT to target,
// whatever host was asked for, and records the hosts asked for.
func convConnectProxy(t *testing.T, target string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var hosts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		hosts = append(hosts, r.Host)
		mu.Unlock()
		upstream, err := net.Dial("tcp", target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			_ = upstream.Close()
			return
		}
		client, _, err := hj.Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
		go func() { _, _ = io.Copy(client, upstream); _ = client.Close() }()
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), hosts...)
	}
}

// convRunChild reruns the named test in a child process with env added and
// returns its combined output, failing when the child fails.
func convRunChild(t *testing.T, name string, env ...string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "child test failed:\n%s", out)
	require.Contains(t, string(out), "--- PASS: "+name, "the child did not run the test:\n%s", out)
}

// WithAzureClientSecret, and the default chain reading AZURE_CLIENT_SECRET,
// authenticate the deployment request with a token for the caller's tenant
// and client. The Entra ID stand-in needs TLS, so the request side runs in a
// child process that trusts its certificate.
func TestConv_AzureClientSecretTokenReachesTheDeployment(t *testing.T) {
	if mode := os.Getenv("CONV_AZURE_CHILD"); mode != "" {
		convAzureChild(t, mode)
		return
	}
	if runtime.GOOS == "darwin" {
		// The child trusts the stand-in through SSL_CERT_FILE, which macOS's
		// platform verifier ignores. CI (Linux) runs this test.
		t.Skip("SSL_CERT_FILE is ignored on macOS")
	}
	for _, mode := range []string{"option", "environment"} {
		t.Run(mode, func(t *testing.T) {
			srv, caPath, rec := convEntraServer(t)
			proxy, proxied := convConnectProxy(t, strings.TrimPrefix(srv.URL, "https://"))
			// The child reaches Entra ID only through the proxy, which ends
			// at the stand-in; the deployment on 127.0.0.1 is not proxied.
			convRunChild(t, "TestConv_AzureClientSecretTokenReachesTheDeployment",
				"CONV_AZURE_CHILD="+mode, "SSL_CERT_FILE="+caPath, "HTTPS_PROXY="+proxy.URL,
				"NO_PROXY=127.0.0.1,localhost", "AZURE_AUTHORITY_HOST=")
			for _, h := range proxied() {
				assert.Equal(t, convEntraHost+":443", h, "only Entra ID is reached through the proxy")
			}

			var token *convCredRequest
			for _, r := range rec.requests() {
				if strings.HasSuffix(r.Path, "/oauth2/v2.0/token") {
					token = &r
				}
			}
			require.NotNil(t, token, "no token request reached Entra ID: %+v", rec.requests())
			assert.Equal(t, "/tenant-1/oauth2/v2.0/token", token.Path)
			assert.Equal(t, "client-2", token.Form.Get("client_id"))
			assert.Equal(t, "secret-3", token.Form.Get("client_secret"))
			assert.Equal(t, "client_credentials", token.Form.Get("grant_type"))
			assert.Contains(t, token.Form.Get("scope"), "cognitiveservices.azure.com")
		})
	}
}

// convAzureChild is the request side of the client-secret test.
func convAzureChild(t *testing.T, mode string) {
	var opts []PlatformOption
	if mode == "option" {
		opts = append(opts, WithAzureClientSecret("tenant-1", "client-2", "secret-3"))
	} else {
		t.Setenv("AZURE_TENANT_ID", "tenant-1")
		t.Setenv("AZURE_CLIENT_ID", "client-2")
		t.Setenv("AZURE_CLIENT_SECRET", "secret-3")
	}
	w := newConvWire(t)
	conv, err := Open(createTestPackFile(t, convChatPack), "chat", WithSkipSchemaValidation(),
		WithAzure(w.URL, "openai", "gpt-4o", opts...))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	_, sendErr := conv.Send(context.Background(), "hi")
	require.NotEmpty(t, w.requests(""), "no request reached the deployment: %v", sendErr)
	r := w.last(t)
	assert.Equal(t, "/openai/deployments/gpt-4o/chat/completions", r.Path)
	assert.Equal(t, "Bearer CSTOKEN", r.Header.Get("Authorization"))
}
