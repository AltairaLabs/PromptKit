package mcp

import (
	"context"
	"net/http"
)

// Test shims: the request helpers these tests were written against moved
// into session (the protocol) and conn (the transport). Each shim performs
// the same exchange through the new layers, so the tests keep exercising the
// real transports.

func (c *StdioClient) sendRequest(ctx context.Context, method string, params, result any) error {
	return c.sess.call(ctx, method, params, result, callOpts{noReinit: true})
}

func (c *StdioClient) sendRequestWithRetry(ctx context.Context, method string, params, result any) error {
	return c.sess.call(ctx, method, params, result, callOpts{idempotent: true, noReinit: true})
}

func (c *StdioClient) sendNotification(method string, params any) error {
	return c.sess.notify(context.Background(), method, params)
}

// sendViaConn sends one request over a bare conn and decodes the result.
func sendViaConn(ctx context.Context, cn conn, id int64, method string, params, out any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	resp, err := cn.send(ctx, &request{id: id, method: method, params: raw, header: http.Header{}})
	if err != nil {
		return err
	}
	return decodeResult(resp, out)
}

func notifyViaConn(ctx context.Context, cn conn, method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	return cn.notify(ctx, &request{method: method, params: raw, header: http.Header{}})
}

var testRequestID int64

func nextTestID() int64 {
	testRequestID++
	return testRequestID
}

func (t *streamableTransport) sendRequest(ctx context.Context, method string, params, out any) error {
	return sendViaConn(ctx, t, nextTestID(), method, params, out)
}

func (t *streamableTransport) sendNotification(ctx context.Context, method string, params any) error {
	return notifyViaConn(ctx, t, method, params)
}

func (t *sseTransport) sendRequest(ctx context.Context, method string, params, out any) error {
	return sendViaConn(ctx, t, nextTestID(), method, params, out)
}

func (t *sseTransport) sendNotification(ctx context.Context, method string, params any) error {
	return notifyViaConn(ctx, t, method, params)
}

// startReadLoop is a no-op: connect starts the read loop now.
func (t *sseTransport) startReadLoop() {}
