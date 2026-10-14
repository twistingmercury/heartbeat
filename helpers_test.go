package heartbeat_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/heartbeat/v2"
)

// slowDelay and slowThreshold let tests exercise slow-response warnings
// without waiting for the three-second default threshold.
const (
	slowDelay     = 150 * time.Millisecond
	slowThreshold = 50 * time.Millisecond
)

func testServer(status int, delay bool) *httptest.Server {
	return httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if delay {
				time.Sleep(slowDelay)
			}
			w.WriteHeader(status)
			_, err := fmt.Fprintln(w, "Hello, client")
			if err != nil {
				return
			}
		}))
}

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
