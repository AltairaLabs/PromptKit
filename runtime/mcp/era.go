package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// MCP 2026-07-28 made the protocol stateless: no initialize handshake, and
// every request carries its protocol version and the client's identity and
// capabilities in _meta. Revisions through 2025-11-25 are "legacy" (they
// handshake); 2026-07-28 and later are "modern". The client speaks both
// ("dual-era", basic/versioning) and detects which a server speaks.

// modernProtocolVersions are the stateless revisions the client speaks,
// newest first.
var modernProtocolVersions = []string{ProtocolVersion}

const (
	methodServerDiscover = "server/discover"

	metaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	metaClientInfo         = "io.modelcontextprotocol/clientInfo"
	metaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	metaServerInfo         = "io.modelcontextprotocol/serverInfo"

	// Error codes the 2026-07-28 specification defines. Receiving one
	// identifies a modern server.
	codeHeaderMismatch                  = -32020
	codeMissingRequiredClientCapability = -32021
	codeUnsupportedProtocolVersion      = -32022

	resultTypeComplete      = "complete"
	resultTypeInputRequired = "input_required"

	// defaultEraProbeTimeout bounds the stdio server/discover probe. A legacy
	// stdio server may not answer an unknown request before initialize at
	// all; the probe then times out and the client falls back.
	defaultEraProbeTimeout = 3 * time.Second
)

// era is the protocol generation a session speaks.
type era int

const (
	eraUnknown era = iota
	eraLegacy
	eraModern
)

// isModernErrorCode reports whether code is one only a 2026-07-28 or later
// server sends.
func isModernErrorCode(code int) bool {
	switch code {
	case codeHeaderMismatch, codeMissingRequiredClientCapability, codeUnsupportedProtocolVersion:
		return true
	}
	return false
}

// DiscoverResult is a server's answer to server/discover: its supported
// versions, capabilities and identity (2026-07-28 server/discover).
type DiscoverResult struct {
	SupportedVersions []string                   `json:"supportedVersions"`
	Capabilities      ServerCapabilities         `json:"capabilities"`
	Instructions      string                     `json:"instructions,omitempty"`
	ResultType        string                     `json:"resultType"`
	TTLMs             *int64                     `json:"ttlMs"`
	CacheScope        string                     `json:"cacheScope"`
	Meta              map[string]json.RawMessage `json:"_meta,omitempty"`
}

// unsupportedVersionData is the data of an UnsupportedProtocolVersionError.
type unsupportedVersionData struct {
	Supported []string `json:"supported"`
	Requested string   `json:"requested"`
}

// eraAware is implemented by conns whose wire behavior differs by era
// (sessions, standalone streams and resumption are legacy-only on HTTP).
type eraAware interface {
	setModern(modern bool)
}

// silentOnUnknown is implemented by conns whose legacy servers may never
// answer a request sent before initialize (stdio). For them a probe that
// times out means "legacy"; an HTTP server always answers with a status, so
// there a timeout is just a failure.
type silentOnUnknown interface {
	mayIgnoreEarlyRequests() bool
}

// modernCapable is implemented by conns that can carry the modern protocol.
// The deprecated HTTP+SSE transport cannot.
type modernCapable interface {
	supportsModern() bool
}

// connect establishes the protocol: it detects whether the server is modern
// or legacy and either selects a modern version or runs the legacy handshake.
func (s *session) connect(ctx context.Context) (*InitializeResponse, error) {
	if mc, ok := s.conn.(modernCapable); !ok || !mc.supportsModern() || s.opts.DisableModernProtocol {
		return s.initialize(ctx)
	}
	info, isModern, err := s.probe(ctx)
	if err != nil {
		return nil, err
	}
	if !isModern {
		logger.Debug("MCP server speaks a handshake-era revision", "server", s.name)
		return s.initialize(ctx)
	}
	return info, nil
}

// probe sends server/discover with the preferred modern version. It returns
// isModern=false for anything that identifies a legacy server. An
// UnsupportedProtocolVersionError naming a modern version this client also
// speaks is answered with one retry at that version.
func (s *session) probe(ctx context.Context) (*InitializeResponse, bool, error) {
	version := modernProtocolVersions[0]
	for attempt := 0; attempt < 2; attempt++ {
		info, next, err := s.probeOnce(ctx, version)
		switch {
		case err != nil:
			return nil, false, err
		case info != nil:
			return info, true, nil
		case next == "":
			return nil, false, nil // legacy
		}
		version = next
	}
	return nil, false, fmt.Errorf("mcp: server %s kept rejecting protocol versions", s.name)
}

// probeOnce sends one discovery request. It returns the server's details if
// it is modern, a version to retry with, or neither for a legacy server.
func (s *session) probeOnce(ctx context.Context, version string) (*InitializeResponse, string, error) {
	timeout, silenceIsLegacy := s.probeTimeout()
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	resp, err := s.sendModern(probeCtx, methodServerDiscover, nil, version)
	cancel()
	if err == nil {
		var res DiscoverResult
		err = decodeResult(resp, &res)
		if err == nil {
			if len(res.SupportedVersions) == 0 {
				return nil, "", nil // not a DiscoverResult: a server that predates discovery
			}
			info, isModern, aerr := s.adoptDiscovery(res, version)
			if aerr != nil || !isModern {
				return nil, "", aerr
			}
			return info, "", nil
		}
	}
	return s.classifyProbeFailure(ctx, err, silenceIsLegacy)
}

// probeTimeout bounds the discovery probe, and reports whether running out
// of time identifies a legacy server.
func (s *session) probeTimeout() (time.Duration, bool) {
	if q, ok := s.conn.(silentOnUnknown); ok && q.mayIgnoreEarlyRequests() {
		if s.opts.EraProbeTimeout > 0 {
			return s.opts.EraProbeTimeout, true
		}
		return defaultEraProbeTimeout, true
	}
	if s.opts.InitTimeout > 0 {
		return s.opts.InitTimeout, false
	}
	return DefaultClientOptions().InitTimeout, false
}

// classifyProbeFailure decides what a failed discovery means.
func (s *session) classifyProbeFailure(
	ctx context.Context, err error, silenceIsLegacy bool,
) (*InitializeResponse, string, error) {
	var rpcErr *RPCError
	switch {
	case errors.As(err, &rpcErr) && rpcErr.Code == codeUnsupportedProtocolVersion:
		next, legacy, ok := s.pickVersion(rpcErr)
		switch {
		case !ok:
			return nil, "", fmt.Errorf("mcp: server %s supports none of this client's protocol versions: %w", s.name, err)
		case legacy:
			return nil, "", nil // a dual-era server that shares only handshake revisions with us
		}
		return nil, next, nil
	case errors.As(err, &rpcErr) && isModernErrorCode(rpcErr.Code):
		return nil, "", fmt.Errorf("mcp: server %s rejected discovery: %w", s.name, err)
	case isLegacyIndication(err), silenceIsLegacy && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded):
		return nil, "", nil
	}
	return nil, "", fmt.Errorf("mcp: server discovery failed: %w", err)
}

// isLegacyIndication reports whether a discovery failure identifies a
// legacy server: any JSON-RPC error that is not a modern one, or an HTTP
// client error without a JSON-RPC body. The fallback is deliberately not
// keyed to one error code (basic/transports/stdio).
func isLegacyIndication(err error) bool {
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return !isModernErrorCode(rpcErr.Code)
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status >= http.StatusBadRequest && statusErr.status < http.StatusInternalServerError &&
			statusErr.status != http.StatusUnauthorized && statusErr.status != http.StatusForbidden
	}
	return errors.Is(err, errNoResponse)
}

// adoptDiscovery selects a version from a DiscoverResult.
func (s *session) adoptDiscovery(res DiscoverResult, requested string) (*InitializeResponse, bool, error) {
	version := firstShared(modernProtocolVersions, res.SupportedVersions)
	if version == "" {
		if firstShared(legacyProtocolVersions, res.SupportedVersions) != "" {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("mcp: server %s supports %v; this client supports %v and %v",
			s.name, res.SupportedVersions, modernProtocolVersions, legacyProtocolVersions)
	}
	if version != requested {
		logger.Debug("MCP server prefers another protocol version", "server", s.name, "version", version)
	}
	info := &InitializeResponse{
		ProtocolVersion: version,
		Capabilities:    res.Capabilities,
		Instructions:    res.Instructions,
	}
	if raw, ok := res.Meta[metaServerInfo]; ok {
		_ = json.Unmarshal(raw, &info.ServerInfo)
	}
	s.mu.Lock()
	s.era = eraModern
	s.version = version
	s.serverInfo = info
	s.mu.Unlock()
	if ea, ok := s.conn.(eraAware); ok {
		ea.setModern(true)
	}
	return info, true, nil
}

// pickVersion chooses a version from an UnsupportedProtocolVersionError's
// supported list: a modern one if any is shared, else reports whether a
// legacy one is.
func (s *session) pickVersion(rpcErr *RPCError) (version string, legacy, ok bool) {
	var data unsupportedVersionData
	if len(rpcErr.Data) == 0 || json.Unmarshal(rpcErr.Data, &data) != nil {
		return "", false, false
	}
	if v := firstShared(modernProtocolVersions, data.Supported); v != "" {
		return v, false, true
	}
	if firstShared(legacyProtocolVersions, data.Supported) != "" {
		return "", true, true
	}
	return "", false, false
}

// firstShared returns the first of ours (in preference order) that theirs
// contains.
func firstShared(ours, theirs []string) string {
	for _, v := range ours {
		if slices.Contains(theirs, v) {
			return v
		}
	}
	return ""
}

func (s *session) isModern() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.era == eraModern
}

// requestMeta is the per-request protocol metadata every modern request
// carries (2026-07-28 basic, "_meta").
func (s *session) requestMeta(version string) map[string]any {
	return map[string]any{
		metaProtocolVersion:    version,
		metaClientInfo:         clientInfo(),
		metaClientCapabilities: s.clientCapabilities(),
	}
}

// withMeta merges the protocol metadata into a request's params object,
// keeping any _meta keys the params already carry.
func withMeta(params json.RawMessage, meta map[string]any) (json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if len(params) > 0 && string(params) != jsonNull {
		if err := json.Unmarshal(params, &obj); err != nil {
			return nil, fmt.Errorf("mcp: params are not an object: %w", err)
		}
	}
	merged := map[string]any{}
	if raw, ok := obj["_meta"]; ok {
		if err := json.Unmarshal(raw, &merged); err != nil {
			return nil, fmt.Errorf("mcp: params _meta is not an object: %w", err)
		}
	}
	for k, v := range meta {
		merged[k] = v
	}
	m, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	obj["_meta"] = m
	return json.Marshal(obj)
}

// sendModern sends one request in the modern shape at the given version.
func (s *session) sendModern(
	ctx context.Context, method string, params json.RawMessage, version string,
) (*JSONRPCMessage, error) {
	withM, err := withMeta(params, s.requestMeta(version))
	if err != nil {
		return nil, err
	}
	req := &request{id: s.nextID.Add(1), method: method, params: withM, header: http.Header{}}
	req.header.Set(headerProtocolVersion, version)
	setStandardHeaders(req.header, method, withM)
	return s.conn.send(ctx, req)
}
