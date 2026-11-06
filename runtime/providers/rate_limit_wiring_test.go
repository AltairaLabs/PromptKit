package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// wiringSlowRate refills one token every 1000s, so no test waits for one.
const wiringSlowRate = 0.001

func rateWiringServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func doGet(ctx context.Context, c *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func minuteCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// The request/response and streaming clients draw from one bucket, so a
// Predict and a PredictStream share the provider's limit.
func TestSetRateLimit_LimitsBothClientsFromOneBucket(t *testing.T) {
	srv, hits := rateWiringServer(t)
	bp := NewBaseProvider("p", false, &http.Client{Timeout: 5 * time.Second})
	bp.SetRateLimit(wiringSlowRate, 2)

	require.NoError(t, doGet(t.Context(), bp.GetHTTPClient(), srv.URL))
	require.NoError(t, doGet(t.Context(), bp.GetStreamingHTTPClient(), srv.URL))

	for name, c := range map[string]*http.Client{"request": bp.GetHTTPClient(), "streaming": bp.GetStreamingHTTPClient()} {
		err := doGet(minuteCtx(t), c, srv.URL)
		assert.ErrorIs(t, err, base.ErrRateLimitPastDeadline, "%s client past the burst", name)
	}
	assert.Equal(t, int32(2), hits.Load())
}

// A transport installed after the limit (as CreateProviderFromSpec's
// HTTPTransport swap does) stays behind it, and a second SetRateLimit
// replaces the first instead of stacking.
func TestSetRateLimit_SurvivesTransportSwapAndDoesNotStack(t *testing.T) {
	bp := NewBaseProvider("p", false, &http.Client{})
	assertLimitedBy := func(step string) {
		t.Helper()
		for name, c := range map[string]*http.Client{
			"request": bp.GetHTTPClient(), "streaming": bp.GetStreamingHTTPClient(),
		} {
			rlt, ok := c.Transport.(*base.RateLimitTransport)
			require.True(t, ok, "%s: %s transport is %T", step, name, c.Transport)
			assert.Same(t, bp.RateLimiter(), rlt.Limiter, "%s: %s", step, name)
			assert.Equal(t, http.DefaultTransport, rlt.Next, "%s: %s limits must not nest", step, name)
		}
	}

	bp.SetRateLimit(10, 1)
	bp.SetHTTPTransport(http.DefaultTransport)
	assertLimitedBy("after transport swap")
	bp.SetRateLimit(20, 3)
	assertLimitedBy("after second SetRateLimit")

	bp.SetRateLimit(0, 0)
	assert.Nil(t, bp.RateLimiter())
	assert.Equal(t, http.DefaultTransport, bp.GetHTTPClient().Transport, "zero removes the limit")
}

// MakeRawRequest takes exactly one token per attempt (the transport's), not
// one from an extra wait in front of it.
func TestMakeRawRequest_TakesOneTokenPerRequest(t *testing.T) {
	srv, hits := rateWiringServer(t)
	bp := NewBaseProvider("p", false, &http.Client{Timeout: 5 * time.Second})
	bp.SetRateLimit(wiringSlowRate, 2)
	headers := RequestHeaders{"Content-Type": "application/json"}

	_, err := bp.MakeRawRequest(minuteCtx(t), srv.URL, []byte(`{}`), headers, "Test")
	require.NoError(t, err)
	_, err = bp.MakeRawRequest(minuteCtx(t), srv.URL, []byte(`{}`), headers, "Test")
	require.NoError(t, err, "the second request is still inside a burst of 2")

	_, err = bp.MakeRawRequest(minuteCtx(t), srv.URL, []byte(`{}`), headers, "Test")
	assert.ErrorIs(t, err, base.ErrRateLimitPastDeadline)
	assert.Equal(t, int32(2), hits.Load(), "a limited request is neither sent nor retried")
}

func TestCreateProviderFromSpec_AppliesRateLimit(t *testing.T) {
	const typeName = "test-ratelimit-provider"
	original := providerFactories[typeName]
	t.Cleanup(func() {
		if original != nil {
			providerFactories[typeName] = original
		} else {
			delete(providerFactories, typeName)
		}
	})
	RegisterProviderFactory(typeName, func(spec ProviderSpec) (Provider, error) {
		bp := NewBaseProvider(spec.ID, false, &http.Client{Timeout: 5 * time.Second})
		return &timeoutAwareProvider{BaseProvider: &bp}, nil
	})

	srv, hits := rateWiringServer(t)
	for name, tc := range map[string]struct {
		opts      RateLimitOptions
		wantBurst int // 0: unlimited
	}{
		"set":              {RateLimitOptions{RequestsPerSecond: wiringSlowRate, Burst: 3}, 3},
		"burst defaults 1": {RateLimitOptions{RequestsPerSecond: wiringSlowRate}, 1},
		"zero unlimited":   {RateLimitOptions{}, 0},
		"burst only":       {RateLimitOptions{Burst: 4}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			hits.Store(0)
			p, err := CreateProviderFromSpec(ProviderSpec{ID: "r", Type: typeName, RateLimit: tc.opts})
			require.NoError(t, err)
			bp := p.(*timeoutAwareProvider).BaseProvider
			if tc.wantBurst == 0 {
				assert.Nil(t, bp.RateLimiter())
				for i := 0; i < 10; i++ {
					require.NoError(t, doGet(minuteCtx(t), bp.GetHTTPClient(), srv.URL))
				}
				assert.Equal(t, int32(10), hits.Load())
				return
			}
			require.NotNil(t, bp.RateLimiter())
			for i := 0; i < tc.wantBurst; i++ {
				require.NoError(t, doGet(t.Context(), bp.GetHTTPClient(), srv.URL), "request %d in burst", i)
			}
			err = doGet(minuteCtx(t), bp.GetStreamingHTTPClient(), srv.URL)
			assert.ErrorIs(t, err, base.ErrRateLimitPastDeadline)
			assert.Equal(t, int32(tc.wantBurst), hits.Load())
		})
	}
}
