package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	gosdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/version"
)

// clientImplName identifies PromptKit in the clientInfo it sends.
const clientImplName = "promptkit"

// transportAuto is a URL with no transport named: Streamable HTTP, falling
// back to HTTP+SSE if the server only hosts that.
const transportAuto Transport = "auto"

// errRequestTimeout is the context cause set when a request outlives
// RequestTimeout, as opposed to the caller canceling it.
var errRequestTimeout = errors.New("mcp: request timeout")

// codeRejectedByTransport is the code go-sdk gives a request its transport
// refused to deliver (jsonrpc2.ErrRejected). The error wraps the cause, an
// *AuthError for instance, and is not the server's answer.
const codeRejectedByTransport = -32005

// errProbeTimeout reports that a stdio server did not answer
// server/discover in time.
var errProbeTimeout = errors.New("mcp: server/discover went unanswered")

// sdkClient implements Client over a session of the official Go SDK
// (github.com/modelcontextprotocol/go-sdk), which owns the protocol: version
// negotiation across both eras, transports, multi round-trip requests and
// server requests. sdkClient adapts it to PromptKit's types and adds what
// the SDK leaves to its callers: retries, reconnection, timeouts that pause
// while a user answers, and host-supplied credentials.
type sdkClient struct {
	config    ServerConfig
	options   ClientOptions
	transport Transport

	// answering counts server requests for user input being handled; while
	// one is, request timeouts do not expire. answered is when the last one
	// finished (UnixNano): the server then gets a full timeout to respond.
	answering atomic.Int32
	answered  atomic.Int64

	mu         sync.Mutex
	live       *liveSession
	info       *InitializeResponse
	closed     bool
	reconnects int
}

// liveSession is one connection to the server.
type liveSession struct {
	session *gosdk.ClientSession
	// stop ends the session's context. go-sdk's SSE transport binds its
	// event stream to the context given to Connect, so that context lives
	// as long as the session, not the handshake.
	stop context.CancelFunc
	dead atomic.Bool
}

func (l *liveSession) close() {
	_ = l.session.Close()
	l.stop()
}

func newSDKClient(config ServerConfig, options ClientOptions, transport Transport) *sdkClient {
	return &sdkClient{config: config, options: options, transport: transport}
}

// Initialize connects to the server and agrees the protocol: server/discover
// first, falling back to the initialize handshake. Idempotent.
func (c *sdkClient) Initialize(ctx context.Context) (*InitializeResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClientClosed
	}
	if c.live != nil {
		return c.info, nil
	}
	if err := c.establish(ctx); err != nil {
		return nil, err
	}
	return c.info, nil
}

// establish connects, retrying a failure that is not the server's answer.
// The caller holds c.mu.
func (c *sdkClient) establish(ctx context.Context) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = c.connectOnce(ctx); err == nil {
			return nil
		}
		if attempt >= c.options.MaxRetries || ctx.Err() != nil || !worthRetrying(err) {
			return err
		}
		logger.Warn("MCP connect failed, retrying", "server", c.config.Name, "attempt", attempt+1, "error", err)
		if werr := sleepCtx(ctx, c.options.RetryDelay*time.Duration(1<<uint(attempt))); werr != nil {
			return werr
		}
	}
}

// worthRetrying reports whether a failed connect may succeed if repeated: not
// when the server answered, and not when authorization was refused, which
// would only run the host's Authorizer (and perhaps a user) through it again.
func worthRetrying(err error) bool {
	var rpcErr *RPCError
	var authErr *AuthError
	return !errors.As(err, &rpcErr) && !errors.As(err, &authErr)
}

// connectOnce makes one connection. A stdio server gets EraProbeTimeout to
// answer server/discover; one that stays silent is restarted with the
// initialize handshake, which go-sdk does not do on its own.
func (c *sdkClient) connectOnce(ctx context.Context) error {
	if c.transport != TransportStdio || c.options.DisableModernProtocol {
		return c.dial(ctx, !c.options.DisableModernProtocol, c.options.InitTimeout)
	}
	probe := c.options.EraProbeTimeout
	if probe <= 0 {
		probe = defaultEraProbeTimeout
	}
	if c.options.InitTimeout > 0 && probe > c.options.InitTimeout {
		probe = c.options.InitTimeout
	}
	err := c.dial(ctx, true, probe)
	if !errors.Is(err, errProbeTimeout) {
		return err
	}
	logger.Debug("MCP stdio server did not answer server/discover; restarting it with the handshake",
		"server", c.config.Name)
	return c.dial(ctx, false, c.options.InitTimeout)
}

// dial connects within timeout and adopts the session.
func (c *sdkClient) dial(ctx context.Context, modern bool, timeout time.Duration) error {
	sessionCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	type result struct {
		session *gosdk.ClientSession
		err     error
	}
	done := make(chan result, 1)
	go func() {
		session, err := c.connect(sessionCtx, modern)
		done <- result{session, err}
	}()
	var expired <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		expired = timer.C
	}
	var res result
	select {
	case res = <-done:
	case <-expired:
		stop()
		if modern && c.transport == TransportStdio && timeout < c.options.InitTimeout {
			return errProbeTimeout
		}
		return fmt.Errorf("initialization timeout after %v", timeout)
	case <-ctx.Done():
		stop()
		return ctx.Err()
	}
	if res.err != nil {
		stop()
		return asRPCError(res.err)
	}
	live := &liveSession{session: res.session, stop: stop}
	go func() {
		_ = live.session.Wait()
		live.dead.Store(true)
	}()
	c.live = live
	c.info = initializeResponseFrom(res.session.InitializeResult())
	return nil
}

// connect opens a session over the configured transport.
func (c *sdkClient) connect(ctx context.Context, modern bool) (*gosdk.ClientSession, error) {
	client := gosdk.NewClient(
		&gosdk.Implementation{Name: clientImplName, Version: version.GetVersion()},
		c.clientOptions(),
	)
	opts := &gosdk.ClientSessionOptions{}
	if !modern {
		opts.ProtocolVersion = LegacyProtocolVersion
	}
	switch c.transport {
	case TransportStdio, TransportUnknown:
		return client.Connect(ctx, &gosdk.CommandTransport{Command: c.command(ctx)}, opts)
	case TransportSSE:
		return c.connectSSE(ctx, client, opts)
	case TransportStreamableHTTP:
		return client.Connect(ctx, c.streamableTransport(), opts)
	case transportAuto:
	}
	session, err := client.Connect(ctx, c.streamableTransport(), opts)
	if err == nil {
		return session, nil
	}
	session, sseErr := c.connectSSE(ctx, client, opts)
	if sseErr == nil {
		return session, nil
	}
	return nil, fmt.Errorf("mcp: Streamable HTTP failed (%w); HTTP+SSE failed: %w", err, sseErr)
}

// connectSSE opens an HTTP+SSE session. The configured URL is the SSE
// endpoint (2024-11-05); earlier releases appended "/sse" to it, so that is
// tried second, for configs that name the server's base URL.
func (c *sdkClient) connectSSE(
	ctx context.Context, client *gosdk.Client, opts *gosdk.ClientSessionOptions,
) (*gosdk.ClientSession, error) {
	session, err := client.Connect(ctx, c.sseTransport(c.config.URL), opts)
	base := strings.TrimRight(c.config.URL, "/")
	if err == nil || strings.HasSuffix(base, "/sse") || ctx.Err() != nil {
		return session, err
	}
	session, legacyErr := client.Connect(ctx, c.sseTransport(base+"/sse"), opts)
	if legacyErr != nil {
		return nil, err
	}
	logger.Warn("MCP SSE stream found at the URL plus /sse; configure the SSE endpoint URL itself",
		"server", c.config.Name, "url", base+"/sse")
	return session, nil
}

func (c *sdkClient) clientOptions() *gosdk.ClientOptions {
	opts := &gosdk.ClientOptions{}
	if h := c.options.ElicitationHandler; h != nil {
		opts.ElicitationHandler = func(ctx context.Context, req *gosdk.ElicitRequest) (*gosdk.ElicitResult, error) {
			c.answering.Add(1)
			defer func() {
				c.answered.Store(time.Now().UnixNano())
				c.answering.Add(-1)
			}()
			return c.elicit(ctx, h, req.Params)
		}
	}
	return opts
}

// elicit adapts the host's ElicitationHandler to the SDK's. Defaults are
// filled before the SDK sees the answer: go-sdk v1.8.0 applies them only to
// a non-empty answer, after validating it.
func (c *sdkClient) elicit(
	ctx context.Context, h ElicitationHandler, p *gosdk.ElicitParams,
) (*gosdk.ElicitResult, error) {
	var req ElicitRequest
	if err := convert(p, &req); err != nil {
		return nil, err
	}
	res, err := answerElicitation(ctx, h, c.config.Name, req)
	if err != nil {
		return nil, err
	}
	out := &gosdk.ElicitResult{Action: res.Action}
	if len(res.Content) > 0 {
		if err := json.Unmarshal(res.Content, &out.Content); err != nil {
			return nil, fmt.Errorf("mcp: elicitation content: %w", err)
		}
	}
	return out, nil
}

// command builds the server process. It lives as long as ctx, the session's
// context; Close stops it gracefully first (go-sdk's CommandTransport closes
// stdin, then signals).
func (c *sdkClient) command(ctx context.Context) *exec.Cmd {
	//nolint:gosec // the host configures the server command
	cmd := exec.CommandContext(ctx, c.config.Command, c.config.Args...)
	cmd.Dir = c.config.WorkingDir
	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=/usr/local/bin:/usr/bin:/bin:" + os.Getenv("PATH")
			break
		}
	}
	for k, v := range c.config.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	return cmd
}

// httpClient carries the server's static headers and the host's Authorizer
// on every request.
func (c *sdkClient) httpClient() *http.Client {
	return &http.Client{Transport: &authorizingTransport{
		headers: c.config.Headers,
		doer:    &httpDoer{client: &http.Client{}, auth: c.options.Authorizer, server: c.config.Name},
	}}
}

func (c *sdkClient) streamableTransport() *gosdk.StreamableClientTransport {
	return &gosdk.StreamableClientTransport{Endpoint: c.config.URL, HTTPClient: c.httpClient()}
}

func (c *sdkClient) sseTransport(endpoint string) *gosdk.SSEClientTransport {
	return &gosdk.SSEClientTransport{Endpoint: endpoint, HTTPClient: c.httpClient()}
}

// authorizingTransport adds the server's headers to each request and sends
// it through the Authorizer, which may answer challenges and resend.
type authorizingTransport struct {
	headers map[string]string
	doer    *httpDoer
}

// RoundTrip sends req with the server's headers through the Authorizer.
func (t *authorizingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.doer.do(req)
}

// ListTools lists every tool, following pagination. A tool that can only run
// as a task is left out: the client does not implement tasks.
func (c *sdkClient) ListTools(ctx context.Context) ([]Tool, error) {
	session, err := c.ready()
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := c.requestContext(ctx)
	defer cancel()
	tools := []Tool{}
	for tool, err := range session.Tools(reqCtx, nil) {
		if err != nil {
			err = c.requestError(ctx, reqCtx, err)
			if c.options.EnableGracefulDegradation {
				logger.Warn("MCP tools/list failed, using graceful degradation", "server", c.config.Name, "error", err)
				return []Tool{}, nil
			}
			return nil, err
		}
		var t Tool
		if err := convert(tool, &t); err != nil {
			return nil, fmt.Errorf("mcp: tool %s: %w", tool.Name, err)
		}
		if t.Execution != nil && t.Execution.TaskSupport == taskSupportRequired {
			continue
		}
		tools = append(tools, t)
	}
	return tools, nil
}

// CallTool calls a tool. It is never retried: a tool may have side effects.
func (c *sdkClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error) {
	session, err := c.ready()
	if err != nil {
		return nil, err
	}
	params := &gosdk.CallToolParams{Name: name}
	if len(arguments) > 0 {
		params.Arguments = arguments
	}
	reqCtx, cancel := c.requestContext(ctx)
	defer cancel()
	res, err := session.CallTool(reqCtx, params)
	if err != nil {
		return nil, c.requestError(ctx, reqCtx, err)
	}
	var out ToolCallResponse
	if err := convert(res, &out); err != nil {
		return nil, fmt.Errorf("mcp: tool %s result: %w", name, err)
	}
	return &out, nil
}

// requestError reports a timed-out request as ErrServerUnresponsive and a
// server's JSON-RPC error as *RPCError.
func (c *sdkClient) requestError(ctx, reqCtx context.Context, err error) error {
	if errors.Is(context.Cause(reqCtx), errRequestTimeout) && ctx.Err() == nil {
		return fmt.Errorf("%w: request timeout after %v", ErrServerUnresponsive, c.options.RequestTimeout)
	}
	return asRPCError(err)
}

// requestContext bounds one request by RequestTimeout. The clock does not
// run out while the client is answering the server's request for user
// input: a handshake-era server asks mid-call, and the user's time to
// answer is not the server's time to respond.
func (c *sdkClient) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.options.RequestTimeout <= 0 {
		return ctx, func() {}
	}
	reqCtx, cancel := context.WithCancelCause(ctx)
	go func() {
		timer := time.NewTimer(c.options.RequestTimeout)
		defer timer.Stop()
		for {
			select {
			case <-reqCtx.Done():
				return
			case <-timer.C:
				if wait := c.timeoutRemaining(); wait > 0 {
					timer.Reset(wait)
					continue
				}
				cancel(errRequestTimeout)
				return
			}
		}
	}()
	return reqCtx, func() { cancel(context.Canceled) }
}

// timeoutRemaining is how much longer a request whose timer has fired may
// run: a full timeout while the user is answering, and the rest of a full
// timeout counted from when the last answer was sent.
func (c *sdkClient) timeoutRemaining() time.Duration {
	if c.answering.Load() > 0 {
		return c.options.RequestTimeout
	}
	since := time.Since(time.Unix(0, c.answered.Load()))
	return c.options.RequestTimeout - since
}

// Close ends the session, and a stdio server process. Idempotent.
func (c *sdkClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.live != nil {
		c.live.close()
	}
	return nil
}

// IsAlive reports whether the client is connected and its connection unbroken.
func (c *sdkClient) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live != nil && !c.closed && !c.live.dead.Load()
}

// ready returns the session, reconnecting first if the connection was lost
// and reconnection is allowed.
func (c *sdkClient) ready() (*gosdk.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.closed:
		return nil, ErrClientClosed
	case c.live == nil:
		return nil, ErrClientNotInitialized
	case !c.live.dead.Load():
		return c.live.session, nil
	}
	if err := c.reconnect(); err != nil {
		return nil, err
	}
	return c.live.session, nil
}

// reconnect replaces a lost connection. The caller holds c.mu.
func (c *sdkClient) reconnect() error {
	lost := ErrServerUnresponsive
	if c.transport == TransportStdio {
		lost = ErrProcessDied
	}
	c.live.close()
	for c.reconnects < c.options.MaxReconnectAttempts {
		c.reconnects++
		logger.Warn("MCP connection lost, reconnecting", "server", c.config.Name, "attempt", c.reconnects)
		if err := c.connectOnce(context.Background()); err == nil {
			c.reconnects = 0
			return nil
		}
	}
	return fmt.Errorf("%w: connection to %s lost", lost, c.config.Name)
}

// asRPCError surfaces the server's JSON-RPC error as PromptKit's *RPCError,
// which callers match to tell a server's answer from a transport failure.
func asRPCError(err error) error {
	var werr *jsonrpc.Error
	if errors.As(err, &werr) && werr.Code != codeRejectedByTransport {
		return &RPCError{Code: int(werr.Code), Message: werr.Message, Data: werr.Data}
	}
	return err
}

// convert maps an SDK wire value onto PromptKit's type through its JSON
// encoding: both describe the same spec shape.
func convert(from, to any) error {
	raw, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, to)
}

func initializeResponseFrom(res *gosdk.InitializeResult) *InitializeResponse {
	var out InitializeResponse
	if res != nil {
		_ = convert(res, &out)
	}
	return &out
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
