package heartbeat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/heartbeat/v2"
)

func TestHandlerReturnCritical(t *testing.T) {
	tOK := testServer(200, false)
	tW1 := testServer(300, false)
	tW2 := testServer(200, true)
	tCrit := testServer(500, false)

	defer func() {
		tOK.Close()
		tW1.Close()
		tW2.Close()
		tCrit.Close()
	}()

	deps := []heartbeat.DependencyDescriptor{
		{Connection: tOK.URL, Name: "Test Good 1", Type: "HTTP"},
		{Connection: tCrit.URL, Name: "Test Critical 2", Type: "HTTP"},
		{Connection: tOK.URL, Name: "Test Good 3", Type: "HTTP"},
		{Connection: tW1.URL, Name: "Test Warn 300: SLOW", Type: "HTTP"},
		{Connection: tW2.URL, Name: "Test Warn 300", Type: "HTTP"},
	}

	resp := httptest.NewRecorder()
	r := http.NewServeMux()
	r.Handle("GET /test", heartbeat.Handler("unit-test", deps...))
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(resp, req)

	data := resp.Body.Bytes()
	assert.Greater(t, len(data), 0)

	var hcr heartbeat.Response

	err := json.Unmarshal(data, &hcr)
	assert.NoError(t, err)
	assert.Equal(t, heartbeat.StatusCritical, hcr.Status)
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)

	str := string(data)
	exp := hcr.String()

	assert.Equal(t, exp, str)
}

func TestHandlerWithNoDependenciesIsOK(t *testing.T) {
	w, resp := serveHealth(t, heartbeat.Handler("svc"))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, heartbeat.StatusOK, resp.Status)
}

// TestHandlerWarning tests the StatusWarning switch case in Handler function
// Verifies that dependencies returning only warnings (no critical failures) result in HTTP 200 OK
// This validates "degraded-but-operational" response behavior
func TestHandlerWarning(t *testing.T) {
	tests := []struct {
		name             string
		setupDeps        func(t *testing.T) []heartbeat.DependencyDescriptor
		expectedStatus   heartbeat.Status
		expectedHTTPCode int
		description      string
	}{
		{
			name: "single dependency returning StatusWarning",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				// Create a server that returns 301 (redirect) which triggers StatusWarning
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusMovedPermanently) // 301 = Warning
				}))
				t.Cleanup(ts.Close)

				return []heartbeat.DependencyDescriptor{
					{Connection: ts.URL, Name: "redirect-dependency", Type: "HTTP"},
				}
			},
			expectedStatus:   heartbeat.StatusWarning,
			expectedHTTPCode: http.StatusOK, // 200, not 503
			description:      "Single warning dependency should return HTTP 200 with Warning status",
		},
		{
			name: "multiple dependencies all returning StatusWarning",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				// Create multiple servers returning different warning conditions
				tsRedirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusFound) // 302 = Warning
				}))
				t.Cleanup(tsRedirect.Close)

				tsSlow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(slowDelay) // Slow response = Warning
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(tsSlow.Close)

				return []heartbeat.DependencyDescriptor{
					{Connection: tsRedirect.URL, Name: "redirect-dep-1", Type: "HTTP"},
					{Connection: tsSlow.URL, Name: "slow-dep-2", Type: "HTTP", SlowThreshold: slowThreshold},
				}
			},
			expectedStatus:   heartbeat.StatusWarning,
			expectedHTTPCode: http.StatusOK, // 200, not 503
			description:      "Multiple warning dependencies should return HTTP 200 with Warning status",
		},
		{
			name: "mix of StatusOK and StatusWarning - highest status wins",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				tsOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK) // 200 = OK
				}))
				t.Cleanup(tsOK.Close)

				tsWarning := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusMovedPermanently) // 301 = Warning
				}))
				t.Cleanup(tsWarning.Close)

				return []heartbeat.DependencyDescriptor{
					{Connection: tsOK.URL, Name: "healthy-dep-1", Type: "HTTP"},
					{Connection: tsWarning.URL, Name: "warning-dep-2", Type: "HTTP"},
					{Connection: tsOK.URL, Name: "healthy-dep-3", Type: "HTTP"},
				}
			},
			expectedStatus:   heartbeat.StatusWarning, // Warning is higher than OK
			expectedHTTPCode: http.StatusOK,           // 200, not 503
			description:      "Mix of OK and Warning should return HTTP 200 with Warning status (highest status wins)",
		},
		{
			name: "custom handler returning StatusWarning",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				return []heartbeat.DependencyDescriptor{
					{
						Name: "custom-warning-handler",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							return heartbeat.StatusResult{
								Status:   heartbeat.StatusWarning,
								Resource: "custom-resource",
								Message:  "degraded performance detected",
							}
						},
					},
				}
			},
			expectedStatus:   heartbeat.StatusWarning,
			expectedHTTPCode: http.StatusOK, // 200, not 503
			description:      "Custom handler returning Warning should result in HTTP 200",
		},
		{
			name: "multiple custom handlers all returning StatusWarning",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				return []heartbeat.DependencyDescriptor{
					{
						Name: "custom-warning-1",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							return heartbeat.StatusResult{
								Status:   heartbeat.StatusWarning,
								Resource: "database",
								Message:  "slow query performance",
							}
						},
					},
					{
						Name: "custom-warning-2",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							return heartbeat.StatusResult{
								Status:   heartbeat.StatusWarning,
								Resource: "cache",
								Message:  "cache hit rate low",
							}
						},
					},
				}
			},
			expectedStatus:   heartbeat.StatusWarning,
			expectedHTTPCode: http.StatusOK, // 200, not 503
			description:      "Multiple custom handlers with Warning should return HTTP 200",
		},
		{
			name: "mix of URL and custom handler dependencies - all warnings",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				tsRedirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusTemporaryRedirect) // 307 = Warning
				}))
				t.Cleanup(tsRedirect.Close)

				return []heartbeat.DependencyDescriptor{
					{Connection: tsRedirect.URL, Name: "redirect-url", Type: "HTTP"},
					{
						Name: "custom-warning",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							return heartbeat.StatusResult{
								Status:   heartbeat.StatusWarning,
								Resource: "external-api",
								Message:  "rate limit approaching",
							}
						},
					},
				}
			},
			expectedStatus:   heartbeat.StatusWarning,
			expectedHTTPCode: http.StatusOK, // 200, not 503
			description:      "Mix of URL and custom handlers with Warning should return HTTP 200",
		},
		{
			name: "StatusWarning with multiple OK dependencies - warning takes precedence",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				tsOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(tsOK.Close)

				tsWarning := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusSeeOther) // 303 = Warning
				}))
				t.Cleanup(tsWarning.Close)

				return []heartbeat.DependencyDescriptor{
					{Connection: tsOK.URL, Name: "ok-dep-1", Type: "HTTP"},
					{Connection: tsOK.URL, Name: "ok-dep-2", Type: "HTTP"},
					{Connection: tsOK.URL, Name: "ok-dep-3", Type: "HTTP"},
					{Connection: tsWarning.URL, Name: "warning-dep", Type: "HTTP"},
					{Connection: tsOK.URL, Name: "ok-dep-4", Type: "HTTP"},
				}
			},
			expectedStatus:   heartbeat.StatusWarning,
			expectedHTTPCode: http.StatusOK, // 200, not 503
			description:      "One warning among many OK dependencies should return HTTP 200 with Warning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := tt.setupDeps(t)

			// Set up test router
			resp := httptest.NewRecorder()
			r := http.NewServeMux()

			// Create request
			req, _ := http.NewRequest(http.MethodGet, "/health", nil)

			// Set up handler and serve request
			r.Handle("GET /health", heartbeat.Handler("test-service", deps...))
			r.ServeHTTP(resp, req)

			// Verify HTTP status code is 200 (not 503)
			assert.Equal(t, tt.expectedHTTPCode, resp.Code,
				"%s: Expected HTTP status code %d but got %d",
				tt.description, tt.expectedHTTPCode, resp.Code)

			// Parse response body
			var hcr heartbeat.Response
			err := json.Unmarshal(resp.Body.Bytes(), &hcr)
			assert.NoError(t, err, "%s: Failed to unmarshal response", tt.description)

			// Verify response status field is "Warning"
			assert.Equal(t, tt.expectedStatus, hcr.Status,
				"%s: Expected status %v but got %v",
				tt.description, tt.expectedStatus, hcr.Status)

			// Verify response is well-formed
			assert.NotEmpty(t, hcr.Resource, "%s: Resource should be set", tt.description)
			assert.NotZero(t, hcr.UTCDateTime, "%s: DateTime should be set", tt.description)
			assert.GreaterOrEqual(t, hcr.RequestDuration, float64(0), "%s: RequestDuration should be >= 0", tt.description)

			// Verify dependencies are populated
			assert.NotEmpty(t, hcr.Dependencies, "%s: Should have dependency results", tt.description)
			assert.Equal(t, len(deps), len(hcr.Dependencies),
				"%s: Should have result for each dependency", tt.description)

			// Verify no dependency has Critical status (would have changed HTTP code to 503)
			for _, dep := range hcr.Dependencies {
				assert.NotEqual(t, heartbeat.StatusCritical, dep.Status,
					"%s: No dependency should be Critical in a Warning test", tt.description)
			}

			// Verify at least one dependency has Warning status
			hasWarning := false
			for _, dep := range hcr.Dependencies {
				if dep.Status == heartbeat.StatusWarning {
					hasWarning = true
					break
				}
			}
			assert.True(t, hasWarning,
				"%s: At least one dependency should have Warning status", tt.description)
		})
	}
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

func TestHandlerContextCancellation(t *testing.T) {
	tests := []struct {
		name              string
		setupDeps         func() []heartbeat.DependencyDescriptor
		cancelBeforeServe bool
		expectedStatus    heartbeat.Status
		expectedHTTPCode  int
		cleanupFunc       func()
	}{
		{
			name: "cancelled context with URL dependency",
			setupDeps: func() []heartbeat.DependencyDescriptor {
				// Create a slow server that will be interrupted by cancellation
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(300 * time.Millisecond) // Slow response
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(ts.Close)
				return []heartbeat.DependencyDescriptor{
					{Connection: ts.URL, Name: "slow-dependency", Type: "HTTP", Timeout: 5 * time.Second},
				}
			},
			cancelBeforeServe: true,
			expectedStatus:    heartbeat.StatusCritical,
			expectedHTTPCode:  http.StatusServiceUnavailable,
		},
		{
			name: "cancelled context with custom handler",
			setupDeps: func() []heartbeat.DependencyDescriptor {
				return []heartbeat.DependencyDescriptor{
					{
						Name: "slow-custom-handler",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							// Slow handler that will be interrupted
							time.Sleep(2 * time.Second)
							return heartbeat.StatusResult{
								Status:   heartbeat.StatusOK,
								Resource: "custom-resource",
							}
						},
						Timeout: 100 * time.Millisecond, // Short timeout to trigger timeout message
					},
				}
			},
			cancelBeforeServe: true,
			expectedStatus:    heartbeat.StatusCritical,
			expectedHTTPCode:  http.StatusServiceUnavailable,
		},
		{
			name: "non-cancelled context completes normally",
			setupDeps: func() []heartbeat.DependencyDescriptor {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(ts.Close)
				return []heartbeat.DependencyDescriptor{
					{Connection: ts.URL, Name: "fast-dependency", Type: "HTTP"},
				}
			},
			cancelBeforeServe: false,
			expectedStatus:    heartbeat.StatusOK,
			expectedHTTPCode:  http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := tt.setupDeps()

			// Set up test router
			resp := httptest.NewRecorder()
			r := http.NewServeMux()

			// Create request with cancellable context
			var req *http.Request
			if tt.cancelBeforeServe {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel immediately to simulate client disconnect
				req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "/health", nil)
			} else {
				req, _ = http.NewRequest(http.MethodGet, "/health", nil)
			}

			// Set up handler and request
			r.Handle("GET /health", heartbeat.Handler("test-service", deps...))

			// Serve the request
			start := time.Now()
			r.ServeHTTP(resp, req)
			elapsed := time.Since(start)

			// Verify response
			assert.Equal(t, tt.expectedHTTPCode, resp.Code, "HTTP status code mismatch")

			// A caller that has gone away gets no body, and the handler must not
			// wait for the slow dependencies before returning.
			if tt.cancelBeforeServe {
				assert.Empty(t, resp.Body.Bytes(), "Cancelled request should not get a body")
				assert.Less(t, elapsed, time.Second, "Cancelled request should return without waiting for checks")
				return
			}

			var hcr heartbeat.Response
			err := json.Unmarshal(resp.Body.Bytes(), &hcr)
			assert.NoError(t, err, "Failed to unmarshal response")
			assert.Equal(t, tt.expectedStatus, hcr.Status, "Health status mismatch")
			assert.NotEmpty(t, hcr.Resource, "Resource should be set")
			assert.NotZero(t, hcr.UTCDateTime, "DateTime should be set")
		})
	}
}

// TestHandlerTimeout tests the edge case where a handler completes after timeout expires
// This specifically tests the case where a late result is discarded
// Verifies that late-arriving results don't cause panics or channel deadlocks
func TestHandlerTimeout(t *testing.T) {
	tests := []struct {
		name                string
		handlerDelay        time.Duration
		timeout             time.Duration
		expectedStatus      heartbeat.Status
		expectedMessagePart string
		description         string
	}{
		{
			name:                "handler completes exactly after timeout - race condition test",
			handlerDelay:        100 * time.Millisecond,
			timeout:             50 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "custom handler timeout after 50ms",
			description:         "Handler finishes after timeout expires, result should be safely discarded",
		},
		{
			name:                "handler completes slightly after timeout - narrow race window",
			handlerDelay:        60 * time.Millisecond,
			timeout:             55 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "custom handler timeout after 55ms",
			description:         "Handler finishes just after timeout, verifies buffered channel prevents deadlock",
		},
		{
			name:                "handler completes well after timeout - clear timeout case",
			handlerDelay:        200 * time.Millisecond,
			timeout:             50 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "custom handler timeout after 50ms",
			description:         "Handler takes much longer than timeout, verifies late result is discarded safely",
		},
		{
			name:                "handler completes just before timeout - successful case",
			handlerDelay:        40 * time.Millisecond,
			timeout:             100 * time.Millisecond,
			expectedStatus:      heartbeat.StatusOK,
			expectedMessagePart: "handler completed successfully",
			description:         "Handler finishes before timeout, result should be returned normally",
		},
		{
			name:                "handler with zero timeout uses default - completes after default",
			handlerDelay:        50 * time.Millisecond,
			timeout:             0, // Should use 10s default
			expectedStatus:      heartbeat.StatusOK,
			expectedMessagePart: "handler completed successfully",
			description:         "Zero timeout triggers default 10s timeout, fast handler completes normally",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create handler that sleeps for specified duration before returning
			handler := func() heartbeat.StatusResult {
				time.Sleep(tt.handlerDelay)
				return heartbeat.StatusResult{
					Status:   heartbeat.StatusOK,
					Resource: "test-resource",
					Message:  "handler completed successfully",
				}
			}

			// Execute handler with timeout: a late result must be discarded safely
			result := heartbeat.ExecuteHandlerWithTimeout(context.Background(), handler, tt.timeout)

			// Verify expected status
			assert.Equal(t, tt.expectedStatus, result.Status,
				"%s: Expected status %v but got %v", tt.description, tt.expectedStatus, result.Status)

			// Verify message contains expected text
			assert.Contains(t, result.Message, tt.expectedMessagePart,
				"%s: Expected message to contain '%s', got: '%s'",
				tt.description, tt.expectedMessagePart, result.Message)

			// For timeout cases, verify the result doesn't have handler's original data
			if tt.expectedStatus == heartbeat.StatusCritical {
				// Resource and name should be empty for timeout results
				assert.Empty(t, result.Resource,
					"%s: Timeout result should have empty Resource field", tt.description)
				assert.Empty(t, result.Name,
					"%s: Timeout result should have empty Name field", tt.description)
			}

			// For successful cases, verify handler's data is preserved
			if tt.expectedStatus == heartbeat.StatusOK {
				assert.Equal(t, "test-resource", result.Resource,
					"%s: Successful result should preserve handler's Resource", tt.description)
			}
		})
	}

	// Additional test: verify no goroutine leaks or panics when handler outlives timeout
	t.Run("no goroutine leak when handler outlives timeout", func(t *testing.T) {
		// This test ensures the goroutine cleanup is safe even when handler runs long after timeout
		slowHandler := func() heartbeat.StatusResult {
			// Handler that takes much longer than timeout
			time.Sleep(500 * time.Millisecond)
			return heartbeat.StatusResult{
				Status:   heartbeat.StatusOK,
				Resource: "slow-resource",
				Message:  "this should never be seen",
			}
		}

		base := runtime.NumGoroutine()
		result := heartbeat.ExecuteHandlerWithTimeout(context.Background(), slowHandler, 10*time.Millisecond)

		// Should timeout and return critical status
		assert.Equal(t, heartbeat.StatusCritical, result.Status)
		assert.Contains(t, result.Message, "timeout")

		// The handler goroutine outlives the timeout, but it must exit once
		// the handler returns.
		waitForGoroutines(t, base)
	})

	// Additional test: verify channel doesn't block when result arrives after context cancellation
	t.Run("buffered channel prevents deadlock on late result", func(t *testing.T) {
		// This test specifically verifies that the buffered channel
		// prevents deadlock when the handler tries to send a result after timeout
		handler := func() heartbeat.StatusResult {
			time.Sleep(100 * time.Millisecond)
			// This send should not block even though nobody is receiving
			return heartbeat.StatusResult{
				Status:   heartbeat.StatusOK,
				Resource: "late-result",
				Message:  "arrived too late",
			}
		}

		// Use a very short timeout to ensure handler finishes after
		base := runtime.NumGoroutine()
		result := heartbeat.ExecuteHandlerWithTimeout(context.Background(), handler, 20*time.Millisecond)

		// Should get timeout response
		assert.Equal(t, heartbeat.StatusCritical, result.Status)
		assert.Contains(t, result.Message, "timeout after 20ms")

		// The late send must not block, so the handler goroutine exits.
		waitForGoroutines(t, base)
	})
}

// TestHandlerPanic verifies that panics in custom handlers are recovered gracefully
// and don't crash the service. Other dependencies should continue to be checked.
func TestHandlerPanic(t *testing.T) {
	tests := []struct {
		name                 string
		setupDeps            func() []heartbeat.DependencyDescriptor
		expectedStatus       heartbeat.Status
		expectedNumResults   int
		panicHandlerName     string
		nonPanicHandlerName  string
		expectedPanicMessage string
		description          string
	}{
		{
			name: "single handler panics - service should not crash",
			setupDeps: func() []heartbeat.DependencyDescriptor {
				return []heartbeat.DependencyDescriptor{
					{
						Name: "panicking-handler",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							panic("intentional panic for testing")
						},
						Timeout: 2 * time.Second,
					},
				}
			},
			expectedStatus:       heartbeat.StatusCritical,
			expectedNumResults:   1,
			panicHandlerName:     "panicking-handler",
			expectedPanicMessage: "panic in custom handler",
			description:          "A single panicking handler should be caught and return critical status",
		},
		{
			name: "one handler panics but others continue checking",
			setupDeps: func() []heartbeat.DependencyDescriptor {
				return []heartbeat.DependencyDescriptor{
					{
						Name: "good-handler-1",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							return heartbeat.StatusResult{
								Status:   heartbeat.StatusOK,
								Resource: "good-resource-1",
								Message:  "completed successfully",
							}
						},
						Timeout: 2 * time.Second,
					},
					{
						Name: "panicking-handler",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							panic("simulated crash in handler")
						},
						Timeout: 2 * time.Second,
					},
					{
						Name: "good-handler-2",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							return heartbeat.StatusResult{
								Status:   heartbeat.StatusOK,
								Resource: "good-resource-2",
								Message:  "also completed successfully",
							}
						},
						Timeout: 2 * time.Second,
					},
				}
			},
			expectedStatus:       heartbeat.StatusCritical,
			expectedNumResults:   3,
			panicHandlerName:     "panicking-handler",
			nonPanicHandlerName:  "good-handler-1",
			expectedPanicMessage: "panic in custom handler",
			description:          "When one handler panics, other handlers should still execute and return results",
		},
		{
			name: "handler panics with nil pointer",
			setupDeps: func() []heartbeat.DependencyDescriptor {
				return []heartbeat.DependencyDescriptor{
					{
						Name: "nil-panic-handler",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							var ptr *string
							// This will panic with nil pointer dereference
							_ = *ptr
							return heartbeat.StatusResult{Status: heartbeat.StatusOK}
						},
						Timeout: 2 * time.Second,
					},
				}
			},
			expectedStatus:       heartbeat.StatusCritical,
			expectedNumResults:   1,
			panicHandlerName:     "nil-panic-handler",
			expectedPanicMessage: "panic in custom handler",
			description:          "Nil pointer panic should be caught and returned as critical status",
		},
		{
			name: "handler panics with error panic",
			setupDeps: func() []heartbeat.DependencyDescriptor {
				return []heartbeat.DependencyDescriptor{
					{
						Name: "error-panic-handler",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							panic(fmt.Errorf("error-based panic"))
						},
						Timeout: 2 * time.Second,
					},
				}
			},
			expectedStatus:       heartbeat.StatusCritical,
			expectedNumResults:   1,
			panicHandlerName:     "error-panic-handler",
			expectedPanicMessage: "panic in custom handler",
			description:          "Error-based panic should be caught and handled properly",
		},
		{
			name: "multiple handlers panic - all should be caught",
			setupDeps: func() []heartbeat.DependencyDescriptor {
				return []heartbeat.DependencyDescriptor{
					{
						Name: "panic-handler-1",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							panic("first panic")
						},
						Timeout: 2 * time.Second,
					},
					{
						Name: "good-handler",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							return heartbeat.StatusResult{
								Status:   heartbeat.StatusOK,
								Resource: "good-resource",
								Message:  "ok",
							}
						},
						Timeout: 2 * time.Second,
					},
					{
						Name: "panic-handler-2",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							panic("second panic")
						},
						Timeout: 2 * time.Second,
					},
				}
			},
			expectedStatus:       heartbeat.StatusCritical,
			expectedNumResults:   3,
			panicHandlerName:     "panic-handler-1",
			nonPanicHandlerName:  "good-handler",
			expectedPanicMessage: "panic in custom handler",
			description:          "Multiple panicking handlers should all be caught independently",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := tt.setupDeps()

			// Execute CheckDependencies - this should NOT crash even if handlers panic
			status, results := heartbeat.CheckDependencies(context.Background(), deps)

			// Verify the overall status is critical (due to panic)
			assert.Equal(t, tt.expectedStatus, status,
				"%s: Expected overall status to be %v", tt.description, tt.expectedStatus)

			// Verify we got results for all dependencies
			assert.Equal(t, tt.expectedNumResults, len(results),
				"%s: Expected %d results but got %d", tt.description, tt.expectedNumResults, len(results))

			// Find the result for the panicking handler
			var panicResult *heartbeat.StatusResult
			for i := range results {
				if results[i].Name == tt.panicHandlerName {
					panicResult = &results[i]
					break
				}
			}

			// Verify panic was caught and converted to critical status
			assert.NotNil(t, panicResult, "%s: Should have result for panicking handler", tt.description)
			if panicResult != nil {
				assert.Equal(t, heartbeat.StatusCritical, panicResult.Status,
					"%s: Panicking handler should return critical status", tt.description)
				assert.Contains(t, panicResult.Message, tt.expectedPanicMessage,
					"%s: Message should indicate panic was recovered, got: %s",
					tt.description, panicResult.Message)
			}

			// If there's a non-panicking handler, verify it completed successfully
			if tt.nonPanicHandlerName != "" {
				var goodResult *heartbeat.StatusResult
				for i := range results {
					if results[i].Name == tt.nonPanicHandlerName {
						goodResult = &results[i]
						break
					}
				}
				assert.NotNil(t, goodResult, "%s: Should have result for good handler", tt.description)
				if goodResult != nil {
					assert.Equal(t, heartbeat.StatusOK, goodResult.Status,
						"%s: Non-panicking handler should complete normally", tt.description)
					assert.NotContains(t, goodResult.Message, "panic",
						"%s: Good handler should not have panic message", tt.description)
				}
			}

			// Verify all results have names set
			for i, result := range results {
				assert.NotEmpty(t, result.Name,
					"%s: Result %d should have name set", tt.description, i)
			}
		})
	}
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

func TestHandlerMachineField(t *testing.T) {
	// Create a test server with OK status
	ts := testServer(200, false)
	defer ts.Close()

	// Create a simple dependency
	deps := []heartbeat.DependencyDescriptor{
		{Connection: ts.URL, Name: "Test Dependency", Type: "HTTP"},
	}

	// Create test context and router
	resp := httptest.NewRecorder()
	r := http.NewServeMux()
	r.Handle("GET /test", heartbeat.Handler("test-service", deps...))
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(resp, req)

	// Parse the response
	var hcr heartbeat.Response
	err := json.Unmarshal(resp.Body.Bytes(), &hcr)
	assert.NoError(t, err)

	// Get expected hostname
	expectedHostname, err := os.Hostname()
	if err != nil {
		// If hostname cannot be determined, it should be empty string
		expectedHostname = ""
	}

	// Verify Machine field is populated with the hostname
	assert.Equal(t, expectedHostname, hcr.Machine,
		"Machine field should be populated with hostname")

	// Verify Machine field is not nil (even if empty)
	assert.NotNil(t, hcr.Machine,
		"Machine field should not be nil")
}

// TestHandlerNameField verifies that the Name field in the Response struct
// is correctly populated with the svcName parameter passed to Handler.
func TestHandlerNameField(t *testing.T) {
	tests := []struct {
		name             string
		svcName          string
		setupDeps        func(t *testing.T) []heartbeat.DependencyDescriptor
		expectedName     string
		expectedResource string
		description      string
	}{
		{
			name:    "service name is set in both Name and Resource fields",
			svcName: "my-service",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(ts.Close)
				return []heartbeat.DependencyDescriptor{
					{Connection: ts.URL, Name: "test-dep", Type: "HTTP"},
				}
			},
			expectedName:     "my-service",
			expectedResource: "my-service",
			description:      "Both Name and Resource should be set to svcName parameter",
		},
		{
			name:    "empty service name results in empty Name and Resource",
			svcName: "",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(ts.Close)
				return []heartbeat.DependencyDescriptor{
					{Connection: ts.URL, Name: "test-dep", Type: "HTTP"},
				}
			},
			expectedName:     "",
			expectedResource: "",
			description:      "Empty svcName should result in empty Name and Resource fields",
		},
		{
			name:    "service name with special characters",
			svcName: "service-name_v2.0",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(ts.Close)
				return []heartbeat.DependencyDescriptor{
					{Connection: ts.URL, Name: "test-dep", Type: "HTTP"},
				}
			},
			expectedName:     "service-name_v2.0",
			expectedResource: "service-name_v2.0",
			description:      "Service name with special characters should be preserved exactly",
		},
		{
			name:    "service name with spaces",
			svcName: "My Service Name",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(ts.Close)
				return []heartbeat.DependencyDescriptor{
					{Connection: ts.URL, Name: "test-dep", Type: "HTTP"},
				}
			},
			expectedName:     "My Service Name",
			expectedResource: "My Service Name",
			description:      "Service name with spaces should be preserved exactly",
		},
		{
			name:    "service name with no dependencies",
			svcName: "standalone-service",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				return []heartbeat.DependencyDescriptor{}
			},
			expectedName:     "standalone-service",
			expectedResource: "standalone-service",
			description:      "Name and Resource should be set even when no dependencies are provided",
		},
		{
			name:    "service name with custom handler dependencies",
			svcName: "custom-handler-service",
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				return []heartbeat.DependencyDescriptor{
					{
						Name: "custom-dep",
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							return heartbeat.StatusResult{
								Status:   heartbeat.StatusOK,
								Resource: "custom-resource",
								Message:  "ok",
							}
						},
					},
				}
			},
			expectedName:     "custom-handler-service",
			expectedResource: "custom-handler-service",
			description:      "Name and Resource should be set correctly with custom handler dependencies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := tt.setupDeps(t)

			// Set up test router
			resp := httptest.NewRecorder()
			r := http.NewServeMux()

			// Create request
			req, _ := http.NewRequest(http.MethodGet, "/health", nil)

			// Set up handler and serve request
			r.Handle("GET /health", heartbeat.Handler(tt.svcName, deps...))
			r.ServeHTTP(resp, req)

			// Parse response body
			var hcr heartbeat.Response
			err := json.Unmarshal(resp.Body.Bytes(), &hcr)
			assert.NoError(t, err, "%s: Failed to unmarshal response", tt.description)

			// Verify Name field is set correctly
			assert.Equal(t, tt.expectedName, hcr.Name,
				"%s: Name field should be set to '%s', got '%s'",
				tt.description, tt.expectedName, hcr.Name)

			// Verify Resource field is also set correctly (both should match svcName)
			assert.Equal(t, tt.expectedResource, hcr.Resource,
				"%s: Resource field should be set to '%s', got '%s'",
				tt.description, tt.expectedResource, hcr.Resource)

			// Verify Name and Resource fields match each other
			assert.Equal(t, hcr.Name, hcr.Resource,
				"%s: Name and Resource fields should match, Name='%s', Resource='%s'",
				tt.description, hcr.Name, hcr.Resource)
		})
	}
}
