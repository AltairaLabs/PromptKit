package a2a_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/testutil"
)

// ReadServerRequest's rules, seen over HTTP through the mock server, which
// reads requests with it as the PromptKit server does.

// echoServer starts a mock that answers every SendMessage with "ok".
func echoServer(t *testing.T) *mock.A2AServer {
	t.Helper()
	m := mock.NewA2AServer(&a2a.AgentCard{Name: "echo"},
		mock.WithSkillResponse("", mock.Response{Parts: []a2a.Part{{Text: testutil.Ptr("ok")}}}))
	_, err := m.Start()
	require.NoError(t, err)
	t.Cleanup(m.Close)
	return m
}

// rawEnvelope is a JSON-RPC response with its id kept as sent and its error
// as generic JSON.
type rawEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  map[string]any  `json:"result"`
	Error   map[string]any  `json:"error"`
}

// post sends body to the mock with one A2A-Version header line per entry of
// versions, at path (which may carry a query).
func post(t *testing.T, m *mock.A2AServer, path, body string, versions ...string) rawEnvelope {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, m.URL()+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	for _, v := range versions {
		req.Header.Add(a2a.HeaderVersion, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var env rawEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	return env
}

const sendBody = `{"jsonrpc":"2.0","id":7,"method":"%s","params":{"message":{"messageId":"m","role":"ROLE_USER",` +
	`"parts":[{"text":"hi"}]}}}`

func sendOf(method string) string { return strings.Replace(sendBody, "%s", method, 1) }

func errorInfo(reason string, metadata map[string]any) []any {
	info := map[string]any{
		"@type": "type.googleapis.com/google.rpc.ErrorInfo", "reason": reason, "domain": "a2a-protocol.org",
	}
	if metadata != nil {
		info["metadata"] = metadata
	}
	return []any{info}
}

func TestServerRequest_RefusesInvalidEnvelopes(t *testing.T) {
	m := echoServer(t)
	for _, tc := range []struct {
		name, body, id, message string
		code                    int
	}{
		{"jsonrpc missing", `{"id":1,"method":"SendMessage"}`, "1",
			`Invalid Request: "jsonrpc" must be exactly "2.0"`, a2a.ErrCodeInvalidRequest},
		{"jsonrpc 1.0", `{"jsonrpc":"1.0","id":"a","method":"SendMessage"}`, `"a"`,
			`Invalid Request: "jsonrpc" must be exactly "2.0"`, a2a.ErrCodeInvalidRequest},
		{"jsonrpc a number", `{"jsonrpc":2.0,"id":2,"method":"SendMessage"}`, "2",
			`Invalid Request: "jsonrpc" must be exactly "2.0"`, a2a.ErrCodeInvalidRequest},
		{"method missing", `{"jsonrpc":"2.0","id":3}`, "3",
			`Invalid Request: "method" must be a non-empty string`, a2a.ErrCodeInvalidRequest},
		{"method empty", `{"jsonrpc":"2.0","id":4,"method":""}`, "4",
			`Invalid Request: "method" must be a non-empty string`, a2a.ErrCodeInvalidRequest},
		{"method a number", `{"jsonrpc":"2.0","id":5,"method":5}`, "5",
			`Invalid Request: "method" must be a non-empty string`, a2a.ErrCodeInvalidRequest},
		{"id an object", `{"jsonrpc":"2.0","id":{},"method":"SendMessage"}`, "null",
			`Invalid Request: "id" must be a string, a number or null`, a2a.ErrCodeInvalidRequest},
		{"id a boolean", `{"jsonrpc":"2.0","id":true,"method":"SendMessage"}`, "null",
			`Invalid Request: "id" must be a string, a number or null`, a2a.ErrCodeInvalidRequest},
		{"null", `null`, "null", "Invalid Request: the request must be a JSON object", a2a.ErrCodeInvalidRequest},
		{"not an object", `"SendMessage"`, "null", "Invalid Request: the request must be a JSON object",
			a2a.ErrCodeInvalidRequest},
		{"batch", ` [{"jsonrpc":"2.0","id":1,"method":"SendMessage"}]`, "null",
			"Invalid Request: batch requests are not supported", a2a.ErrCodeInvalidRequest},
		{"malformed", `{"jsonrpc":`, "null", "Parse error", a2a.ErrCodeParse},
		{"unknown method", `{"jsonrpc":"2.0","id":6,"method":"tasks/frobnicate"}`, "6",
			"Method not found", a2a.ErrCodeMethodNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := post(t, m, "/a2a", tc.body, "1.0")
			assert.Equal(t, "2.0", env.JSONRPC)
			assert.JSONEq(t, tc.id, string(env.ID))
			require.NotNil(t, env.Error)
			assert.Equal(t, float64(tc.code), env.Error["code"])
			assert.Equal(t, tc.message, env.Error["message"])
			assert.NotContains(t, env.Error, "data")
		})
	}
}

func TestServerRequest_EchoesTheRequestIDExactly(t *testing.T) {
	m := echoServer(t)
	for _, id := range []string{`9007199254740993`, `0`, `"req-1"`, `-1.5`, `null`} {
		body := strings.Replace(sendOf("SendMessage"), `"id":7`, `"id":`+id, 1)
		env := post(t, m, "/a2a", body, "1.0")
		assert.Equal(t, id, string(env.ID))
		assert.Nil(t, env.Error)
	}
}

func TestServerRequest_VersionAndMethodNameMustAgree(t *testing.T) {
	m := echoServer(t)
	versionInfo := func(requested string) []any {
		return errorInfo("VERSION_NOT_SUPPORTED",
			map[string]any{"requestedVersion": requested, "supportedVersions": "1.0,0.3"})
	}
	for _, tc := range []struct {
		name, method string
		versions     []string
		code         int
		message      string
		data         any
	}{
		{"1.0 method without a header", "SendMessage", nil, a2a.ErrCodeVersionNotSupported,
			"Version not supported: SendMessage is an A2A 1.0 method, and a request without A2A-Version is " +
				"A2A 0.3; send A2A-Version: 1.0", versionInfo("0.3")},
		{"1.0 method with a blank header", "SendMessage", []string{" \t"}, a2a.ErrCodeVersionNotSupported,
			"Version not supported: SendMessage is an A2A 1.0 method, and a request without A2A-Version is " +
				"A2A 0.3; send A2A-Version: 1.0", versionInfo("0.3")},
		{"1.0 method with A2A-Version 0.3", "SendMessage", []string{"0.3"}, a2a.ErrCodeVersionNotSupported,
			"Version not supported: SendMessage is an A2A 1.0 method, and the request names A2A 0.3; " +
				"send A2A-Version: 1.0", versionInfo("0.3")},
		{"0.3 method with A2A-Version 1.0", "message/send", []string{"1.0"}, a2a.ErrCodeMethodNotFound,
			"Method not found: message/send is not an A2A 1.0 method (A2A 1.0 names it SendMessage)", nil},
		{"unsupported version", "SendMessage", []string{"2.0"}, a2a.ErrCodeVersionNotSupported,
			`a2a: protocol version "2.0" is not supported (supported: 1.0, 0.3)`, versionInfo("2.0")},
		{"lines naming different versions", "SendMessage", []string{"1.0", "0.3"}, a2a.ErrCodeVersionNotSupported,
			`a2a: A2A-Version names more than one version ("1.0", "0.3")`, versionInfo("1.0, 0.3")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := post(t, m, "/a2a", sendOf(tc.method), tc.versions...)
			assert.Equal(t, "7", string(env.ID))
			require.NotNil(t, env.Error)
			assert.Equal(t, float64(tc.code), env.Error["code"])
			assert.Equal(t, tc.message, env.Error["message"])
			assert.Equal(t, tc.data, env.Error["data"])
		})
	}
}

func TestServerRequest_ServesEachVersionByItsHeader(t *testing.T) {
	m := echoServer(t)
	served := func(env rawEnvelope) map[string]any {
		require.Nil(t, env.Error, "unexpected error %v", env.Error)
		return env.Result
	}

	v1 := served(post(t, m, "/a2a", sendOf("SendMessage"), "1.0"))
	assert.Contains(t, v1, "task", "1.0 wraps the task")
	v1 = served(post(t, m, "/a2a?A2A-Version=1.0", sendOf("SendMessage"), "  "))
	assert.Contains(t, v1, "task", "a blank header falls through to the query parameter")
	v1 = served(post(t, m, "/a2a", sendOf("SendMessage"), "", " 1.0 ", "1.0.3"))
	assert.Contains(t, v1, "task", "every header line counts, blank ones ignored")

	v03 := served(post(t, m, "/a2a", sendOf("message/send")))
	assert.Equal(t, "task", v03["kind"], "no header is 0.3")
	v03 = served(post(t, m, "/a2a?A2A-Version=1.0", sendOf("message/send"), "0.3"))
	assert.Equal(t, "task", v03["kind"], "the header wins over the query parameter")
}

// ErrorFor gives an A2A error its ErrorInfo in 1.0 only — except
// VersionNotSupportedError, which only 1.0 defines — and never a standard
// JSON-RPC error.
func TestErrorFor_ErrorInfoFollowsTheVersion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		v        a2a.ProtocolVersion
		code     int
		metadata map[string]string
		want     string
	}{
		{"1.0 task error", a2a.ProtocolVersion10, a2a.ErrCodeTaskNotFound, map[string]string{"taskId": "t"},
			`{"code":-32001,"message":"m","data":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",` +
				`"reason":"TASK_NOT_FOUND","domain":"a2a-protocol.org","metadata":{"taskId":"t"}}]}`},
		{"1.0 without metadata", a2a.ProtocolVersion10, a2a.ErrCodeUnsupportedOperation, map[string]string{},
			`{"code":-32004,"message":"m","data":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",` +
				`"reason":"UNSUPPORTED_OPERATION","domain":"a2a-protocol.org"}]}`},
		{"0.3 task error", a2a.ProtocolVersion03, a2a.ErrCodeTaskNotFound, map[string]string{"taskId": "t"},
			`{"code":-32001,"message":"m"}`},
		{"0.3 version error", a2a.ProtocolVersion03, a2a.ErrCodeVersionNotSupported, nil,
			`{"code":-32009,"message":"m","data":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",` +
				`"reason":"VERSION_NOT_SUPPORTED","domain":"a2a-protocol.org"}]}`},
		{"1.0 standard error", a2a.ProtocolVersion10, a2a.ErrCodeInvalidParams, map[string]string{"x": "y"},
			`{"code":-32602,"message":"m"}`},
	} {
		raw, err := json.Marshal(a2a.ErrorFor(tc.v, tc.code, "m", tc.metadata))
		require.NoError(t, err)
		assert.JSONEq(t, tc.want, string(raw), tc.name)
	}
}

// RequestVersion reads every A2A-Version header line, blank ones naming
// nothing, before the query parameter; a request naming none is 0.3.
func TestRequestVersion_HeaderLinesAndQuery(t *testing.T) {
	request := func(query string, lines ...string) *http.Request {
		r, err := http.NewRequest(http.MethodPost, "http://agent.example/a2a"+query, http.NoBody)
		require.NoError(t, err)
		for _, line := range lines {
			r.Header.Add(a2a.HeaderVersion, line)
		}
		return r
	}
	for _, tc := range []struct {
		name, query string
		lines       []string
		v           a2a.ProtocolVersion
		named, err  string
	}{
		{"nothing", "", nil, a2a.ProtocolVersion03, "", ""},
		{"blank lines", "", []string{" ", "\t"}, a2a.ProtocolVersion03, "", ""},
		{"blank line, then a version", "", []string{" ", " 1.0 "}, a2a.ProtocolVersion10, "1.0", ""},
		{"blank line, then the query", "?A2A-Version=%201.0", []string{"  "}, a2a.ProtocolVersion10, "1.0", ""},
		{"header over query", "?A2A-Version=1.0", []string{"0.3"}, a2a.ProtocolVersion03, "0.3", ""},
		{"same Major.Minor", "", []string{"1.0", "1.0.1"}, a2a.ProtocolVersion10, "1.0", ""},
		{"different versions", "", []string{"1.0", "0.3"}, "", "1.0, 0.3",
			`a2a: A2A-Version names more than one version ("1.0", "0.3")`},
		{"unsupported", "", []string{"9.9"}, "", "9.9",
			`a2a: protocol version "9.9" is not supported (supported: 1.0, 0.3)`},
	} {
		v, named, err := a2a.RequestVersion(request(tc.query, tc.lines...))
		assert.Equal(t, tc.v, v, tc.name)
		assert.Equal(t, tc.named, named, tc.name)
		if tc.err == "" {
			assert.NoError(t, err, tc.name)
		} else {
			assert.EqualError(t, err, tc.err, tc.name)
		}
	}
}
