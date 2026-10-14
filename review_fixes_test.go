package heartbeat_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/heartbeat/v2"
)

func serveHealth(t *testing.T, h http.Handler) (*httptest.ResponseRecorder, heartbeat.Response) {
	t.Helper()
	r := http.NewServeMux()
	r.Handle("GET /health", h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	var resp heartbeat.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return w, resp
}

// waitForGoroutines polls until the goroutine count drops to at most want.
func waitForGoroutines(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.LessOrEqual(t, runtime.NumGoroutine(), want, "goroutines leaked")
}

func TestCredentialsAreRedacted(t *testing.T) {
	ts := testServer(http.StatusOK, false)
	defer ts.Close()
	withCreds := strings.Replace(ts.URL, "http://", "http://admin:s3cret@", 1)

	result := heartbeat.CheckURL(context.Background(), withCreds, time.Second)
	assert.Equal(t, heartbeat.StatusOK, result.Status)
	assert.NotContains(t, result.Resource, "s3cret")
	assert.Contains(t, result.Resource, "admin:xxxxx@")

	d := heartbeat.DependencyDescriptor{Name: "dep", Connection: withCreds}
	assert.NotContains(t, d.String(), "s3cret")

	bad := heartbeat.CheckURL(context.Background(), "http://admin:s3cret@[::1", time.Second)
	assert.Equal(t, heartbeat.StatusCritical, bad.Status)
	assert.NotContains(t, bad.Message+bad.Resource, "s3cret")
}

func TestTransportErrorsDoNotExposeAddresses(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	result := heartbeat.CheckURL(context.Background(), "http://"+addr+"/", time.Second)
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "HTTP request failed")
	assert.NotContains(t, result.Message, addr)
}

func TestPanicValueIsNotReturned(t *testing.T) {
	result := heartbeat.ExecuteHandlerWithTimeout(context.Background(), func() heartbeat.StatusResult {
		panic("dsn=postgres://u:pw@db:5432")
	}, time.Second)
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "panic")
	assert.NotContains(t, result.Message, "pw@db")
}

func TestHandlerFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		status  heartbeat.Status
		message string
	}{
		{"zero value status", heartbeat.StatusNotSet, "check returned no status"},
		{"out of range status", heartbeat.Status(7), "check returned invalid status 7"},
		{"negative status", heartbeat.Status(-1), "check returned invalid status -1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := heartbeat.Handler("svc", heartbeat.DependencyDescriptor{
				Name: "dep",
				CheckFunc: func(context.Context) heartbeat.StatusResult {
					return heartbeat.StatusResult{Status: tt.status, Message: "details"}
				},
			})
			w, resp := serveHealth(t, h)
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
			assert.Equal(t, heartbeat.StatusCritical, resp.Status)
			require.Len(t, resp.Dependencies, 1)
			assert.Equal(t, heartbeat.StatusCritical, resp.Dependencies[0].Status)
			assert.Equal(t, tt.message+": details", resp.Dependencies[0].Message)
		})
	}
}

func TestHandlerWithNoDependenciesIsOK(t *testing.T) {
	w, resp := serveHealth(t, heartbeat.Handler("svc"))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, heartbeat.StatusOK, resp.Status)
}

func TestHandlerRejectsInvalidDescriptors(t *testing.T) {
	ok := func(context.Context) heartbeat.StatusResult {
		return heartbeat.StatusResult{Status: heartbeat.StatusOK}
	}
	tests := []struct {
		name string
		dep  heartbeat.DependencyDescriptor
	}{
		{"nothing to check", heartbeat.DependencyDescriptor{Name: "x"}},
		{"unsupported scheme", heartbeat.DependencyDescriptor{Name: "x", Connection: "ftp://example.com"}},
		{"missing host", heartbeat.DependencyDescriptor{Name: "x", Connection: "http://"}},
		{"unparseable URL", heartbeat.DependencyDescriptor{Name: "x", Connection: "http://[::1"}},
		{"negative timeout", heartbeat.DependencyDescriptor{Name: "x", CheckFunc: ok, Timeout: -time.Second}},
		{"negative slow threshold", heartbeat.DependencyDescriptor{Name: "x", Connection: "http://example.com", SlowThreshold: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, func() { heartbeat.Handler("svc", tt.dep) })
		})
	}

	assert.NotPanics(t, func() {
		heartbeat.Handler("svc",
			heartbeat.DependencyDescriptor{Name: "url", Connection: "https://example.com/health"},
			heartbeat.DependencyDescriptor{Name: "check", CheckFunc: ok},
		)
	})
}

func TestCheckFuncTakesPrecedence(t *testing.T) {
	var legacyCalled atomic.Bool
	_, results := heartbeat.CheckDependencies(context.Background(), []heartbeat.DependencyDescriptor{{
		Name:       "dep",
		Connection: "http://127.0.0.1:1/",
		CheckFunc: func(context.Context) heartbeat.StatusResult {
			return heartbeat.StatusResult{Status: heartbeat.StatusOK}
		},
		HandlerFunc: func() heartbeat.StatusResult {
			legacyCalled.Store(true)
			return heartbeat.StatusResult{Status: heartbeat.StatusCritical}
		},
	}})
	assert.Equal(t, heartbeat.StatusOK, results[0].Status)
	assert.False(t, legacyCalled.Load())
}

func TestCheckFuncIsCancelledOnTimeout(t *testing.T) {
	base := runtime.NumGoroutine()
	for range 50 {
		result := heartbeat.RunCheck(context.Background(), func(ctx context.Context) heartbeat.StatusResult {
			<-ctx.Done()
			return heartbeat.StatusResult{Status: heartbeat.StatusOK}
		}, 5*time.Millisecond)
		assert.Equal(t, heartbeat.StatusCritical, result.Status)
		assert.Contains(t, result.Message, "timeout after 5ms")
	}
	waitForGoroutines(t, base)
}

func TestCustomCheckDurationIsRecorded(t *testing.T) {
	result := heartbeat.RunCheck(context.Background(), func(context.Context) heartbeat.StatusResult {
		time.Sleep(20 * time.Millisecond)
		return heartbeat.StatusResult{Status: heartbeat.StatusOK}
	}, time.Second)
	assert.GreaterOrEqual(t, result.RequestDuration, 20.0)
}

func TestRealRedirectIsFollowed(t *testing.T) {
	target := testServer(http.StatusOK, false)
	defer target.Close()
	redirect := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusMovedPermanently))
	defer redirect.Close()

	result := heartbeat.CheckURL(context.Background(), redirect.URL, time.Second)
	assert.Equal(t, heartbeat.StatusOK, result.Status)
	assert.Equal(t, http.StatusOK, result.StatusCode)
}

func TestConnectionsAreReused(t *testing.T) {
	var conns atomic.Int32
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}))
	ts.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	ts.Start()
	defer ts.Close()

	for range 5 {
		result := heartbeat.CheckURL(context.Background(), ts.URL, time.Second)
		require.Equal(t, heartbeat.StatusOK, result.Status)
	}
	assert.Equal(t, int32(1), conns.Load(), "each check should reuse the same connection")
}

func TestConcurrentRequestsShareOneCheck(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(100 * time.Millisecond)
	}))
	defer ts.Close()

	r := http.NewServeMux()
	r.Handle("GET /health", heartbeat.Handler("svc", heartbeat.DependencyDescriptor{Name: "dep", Connection: ts.URL}))

	var wg sync.WaitGroup
	codes := make([]int, 50)
	for i := range codes {
		wg.Go(func() {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
			codes[i] = w.Code
		})
	}
	wg.Wait()

	assert.Equal(t, int32(1), hits.Load(), "concurrent probes should share one dependency call")
	for _, c := range codes {
		assert.Equal(t, http.StatusOK, c)
	}

	// A later request starts a fresh run.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int32(2), hits.Load())
}

func TestHandlerServesJSONOnServeMux(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /health", heartbeat.Handler("svc"))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/health", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
