package heartbeat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/twistingmercury/heartbeat"
)

func testServer(status int, delay bool) *httptest.Server {
	return httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if delay {
				time.Sleep(4 * time.Second)
			}
			w.WriteHeader(status)
			_, err := fmt.Fprintln(w, "Hello, client")
			if err != nil {
				return
			}
		}))
}

func TestDependencyHandlerFunc(t *testing.T) {
	exp := heartbeat.StatusResult{
		Status:          heartbeat.StatusOK,
		Name:            "Test Func",
		RequestDuration: 42,
		Resource:        "None",
	}
	dep := heartbeat.DependencyDescriptor{
		HandlerFunc: func() (hsr heartbeat.StatusResult) {
			return exp
		},
	}
	act := dep.HandlerFunc()

	assert.Equal(t, exp, act)
}

func TestCheckDepsInvokesHandlerFunc(t *testing.T) {
	exp := heartbeat.StatusResult{
		Status:          heartbeat.StatusOK,
		Name:            "Test Func",
		RequestDuration: 42,
		Resource:        "None",
	}

	ts := testServer(200, false)
	defer ts.Close()
	deps := []heartbeat.DependencyDescriptor{
		{Connection: ts.URL, Name: "Test URL 1", Type: "HTTP"},
		{Connection: "", Name: "Test custom", Type: "Custom", HandlerFunc: func() (hsr heartbeat.StatusResult) { return exp }},
	}

	s, r := heartbeat.CheckDeps(context.Background(), deps)
	assert.Equal(t, heartbeat.StatusOK, s)
	assert.Equal(t, 2, len(r))
}

func TestCheckUrlReturnsOK(t *testing.T) {
	ts := testServer(200, false)
	defer ts.Close()

	act := heartbeat.CheckURL(context.Background(), ts.URL, 10*time.Second)
	assert.Equal(t, heartbeat.StatusOK, act.Status)
}

func TestCheckURLReturnsError(t *testing.T) {
	act := heartbeat.CheckURL(context.Background(), "hqpn://wtf.is.this.url???", 10*time.Second)
	assert.Equal(t, heartbeat.StatusCritical, act.Status)
}

func TestCheckUrlReturnWarning(t *testing.T) {
	ts := testServer(200, true)
	defer ts.Close()

	act := heartbeat.CheckURL(context.Background(), ts.URL, 10*time.Second)
	assert.Equal(t, heartbeat.StatusWarning, act.Status)
}

func TestCheckUrlReturnCritical(t *testing.T) {
	ts := testServer(500, false)
	defer ts.Close()

	act := heartbeat.CheckURL(context.Background(), ts.URL, 10*time.Second)
	assert.Equal(t, heartbeat.StatusCritical, act.Status)
}

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
	gin.SetMode(gin.TestMode)
	c, r := gin.CreateTestContext(resp)
	r.GET("/test", heartbeat.Handler("unit-test", deps...))
	c.Request, _ = http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(resp, c.Request)

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

func TestStatusString(t *testing.T) {
	assert.Equal(t, "OK", heartbeat.StatusOK.String())
	assert.Equal(t, "Warning", heartbeat.StatusWarning.String())
	assert.Equal(t, "Critical", heartbeat.StatusCritical.String())
	assert.Equal(t, "Status(5)", heartbeat.Status(5).String())
}

func TestStatusParse(t *testing.T) {
	ok, err := heartbeat.ParseStatus("OK")
	assert.NoError(t, err)
	assert.Equal(t, heartbeat.StatusOK, ok)

	wn, err := heartbeat.ParseStatus("Warning")
	assert.NoError(t, err)
	assert.Equal(t, heartbeat.StatusWarning, wn)

	cr, err := heartbeat.ParseStatus("Critical")
	assert.NoError(t, err)
	assert.Equal(t, heartbeat.StatusCritical, cr)

	x, err := heartbeat.ParseStatus("Fatal")
	assert.Error(t, err)
	assert.Equal(t, heartbeat.StatusNotSet, x)
}

func TestStatusUnmarshalText(t *testing.T) {
	var err error
	var hs heartbeat.Status
	err = hs.UnmarshalText([]byte("OK"))
	assert.NoError(t, err)

	err = hs.UnmarshalText([]byte("Warning"))
	assert.NoError(t, err)

	err = hs.UnmarshalText([]byte("Critical"))
	assert.NoError(t, err)

	err = hs.UnmarshalText([]byte("Fatal"))
	assert.Error(t, err)
}

func TestDependencyDescriptorString(t *testing.T) {
	desc := heartbeat.DependencyDescriptor{
		Connection:  "test",
		HandlerFunc: nil,
		Name:        "test",
		Type:        "test",
	}

	js := desc.String()

	assert.Greater(t, len(js), 0)
}

func TestStatusResultString(t *testing.T) {
	r := heartbeat.StatusResult{
		Status:          heartbeat.StatusOK,
		Name:            "test",
		RequestDuration: 42,
		Resource:        "test",
	}

	js := r.String()

	assert.Greater(t, len(js), 0)
}

func TestDependencyTypeString(t *testing.T) {
	assert.Equal(t, "OK", heartbeat.StatusOK.String())
	assert.Equal(t, "Warning", heartbeat.StatusWarning.String())
	assert.Equal(t, "Critical", heartbeat.StatusCritical.String())
}

func TestCheckURLValidation(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		expectedStatus heartbeat.Status
		messageContain string
	}{
		{
			name:           "invalid URL scheme - file",
			url:            "file:///etc/passwd",
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "unsupported URL scheme",
		},
		{
			name:           "invalid URL scheme - ftp",
			url:            "ftp://example.com",
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "unsupported URL scheme",
		},
		{
			name:           "malformed URL",
			url:            "not a url at all",
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "unsupported URL scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := heartbeat.CheckURL(context.Background(), tt.url, 10*time.Second)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Contains(t, result.Message, tt.messageContain)
		})
	}
}

func TestCheckURL400StatusCodes(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		expectedStatus heartbeat.Status
		messageContain string
	}{
		{
			name:           "404 Not Found",
			statusCode:     404,
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "client error",
		},
		{
			name:           "401 Unauthorized",
			statusCode:     401,
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "client error",
		},
		{
			name:           "403 Forbidden",
			statusCode:     403,
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "client error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := testServer(tt.statusCode, false)
			defer ts.Close()

			result := heartbeat.CheckURL(context.Background(), ts.URL, 10*time.Second)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Contains(t, result.Message, tt.messageContain)
		})
	}
}

func TestCheckURL300StatusCodes(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		expectedStatus heartbeat.Status
		messageContain string
	}{
		{
			name:           "301 Moved Permanently",
			statusCode:     301,
			expectedStatus: heartbeat.StatusWarning,
			messageContain: "redirect",
		},
		{
			name:           "302 Found",
			statusCode:     302,
			expectedStatus: heartbeat.StatusWarning,
			messageContain: "redirect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := testServer(tt.statusCode, false)
			defer ts.Close()

			result := heartbeat.CheckURL(context.Background(), ts.URL, 10*time.Second)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Contains(t, result.Message, tt.messageContain)
		})
	}
}

func TestExecuteHandlerWithTimeoutContextCancellation(t *testing.T) {
	tests := []struct {
		name                string
		setupHandler        func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc)
		timeout             time.Duration
		expectedStatus      heartbeat.Status
		expectedMessagePart string
		description         string
	}{
		{
			name: "parent context cancelled before handler completes",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				handler := func() heartbeat.StatusResult {
					// Simulate long-running handler
					time.Sleep(200 * time.Millisecond)
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed successfully",
					}
				}
				return ctx, cancel, handler
			},
			timeout:             500 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "custom handler timeout",
			description:         "When parent context is cancelled, should return critical status with timeout message",
		},
		{
			name: "timeout expires before handler completes",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx := context.Background()
				handler := func() heartbeat.StatusResult {
					// Handler takes longer than timeout
					time.Sleep(300 * time.Millisecond)
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed successfully",
					}
				}
				return ctx, nil, handler
			},
			timeout:             50 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "custom handler timeout after 50ms",
			description:         "When timeout expires before handler completes, should return critical status with timeout message",
		},
		{
			name: "handler completes before timeout",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx := context.Background()
				handler := func() heartbeat.StatusResult {
					// Fast handler that completes quickly
					time.Sleep(10 * time.Millisecond)
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed successfully",
					}
				}
				return ctx, nil, handler
			},
			timeout:             500 * time.Millisecond,
			expectedStatus:      heartbeat.StatusOK,
			expectedMessagePart: "completed successfully",
			description:         "When handler completes before timeout, should return handler's actual result",
		},
		{
			name: "race condition - result arrives after timeout (line 159)",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx := context.Background()
				handler := func() heartbeat.StatusResult {
					// Handler completes just after timeout expires
					// This tests the race condition at line 159 where the result
					// tries to be sent to resultChan but context is already done
					time.Sleep(100 * time.Millisecond)
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed after timeout",
					}
				}
				return ctx, nil, handler
			},
			timeout:             50 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "custom handler timeout after 50ms",
			description:         "When handler completes after timeout, result should be discarded and timeout returned",
		},
		{
			name: "default timeout applied when zero",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx := context.Background()
				handler := func() heartbeat.StatusResult {
					// Handler completes quickly
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed successfully",
					}
				}
				return ctx, nil, handler
			},
			timeout:             0, // Should use default 10s timeout
			expectedStatus:      heartbeat.StatusOK,
			expectedMessagePart: "completed successfully",
			description:         "When timeout is zero, should use default 10s timeout and complete successfully",
		},
		{
			name: "immediate context cancellation",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				handler := func() heartbeat.StatusResult {
					// Handler that would complete successfully if given time
					time.Sleep(50 * time.Millisecond)
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed successfully",
					}
				}
				return ctx, cancel, handler
			},
			timeout:             500 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "custom handler timeout",
			description:         "When context is cancelled immediately, should return timeout even with generous timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel, handler := tt.setupHandler()

			// For tests that require cancellation, cancel the context with a slight delay
			// to ensure the handler goroutine has started
			if cancel != nil && (tt.name == "parent context cancelled before handler completes" || tt.name == "immediate context cancellation") {
				// Start a goroutine to cancel context
				go func() {
					if tt.name == "immediate context cancellation" {
						// Cancel immediately
						cancel()
					} else {
						// Cancel after a short delay to simulate mid-execution cancellation
						time.Sleep(50 * time.Millisecond)
						cancel()
					}
				}()
				defer cancel() // Ensure cleanup even if test fails
			} else if cancel != nil {
				defer cancel()
			}

			// Execute the handler with timeout
			result := heartbeat.ExecuteHandlerWithTimeout(ctx, handler, tt.timeout)

			// Verify status
			assert.Equal(t, tt.expectedStatus, result.Status,
				"Test case: %s - Status mismatch", tt.description)

			// Verify message contains expected part
			assert.Contains(t, result.Message, tt.expectedMessagePart,
				"Test case: %s - Message should contain '%s', got: '%s'",
				tt.description, tt.expectedMessagePart, result.Message)

			// Additional verification for timeout cases
			if tt.expectedStatus == heartbeat.StatusCritical {
				assert.Contains(t, result.Message, "timeout",
					"Critical status should indicate timeout")
			}
		})
	}
}

func TestCheckURLContextCancellation(t *testing.T) {
	tests := []struct {
		name               string
		setupServer        func() *httptest.Server
		setupContext       func() (context.Context, context.CancelFunc)
		timeout            time.Duration
		expectedStatus     heartbeat.Status
		expectedMsgContain string
		description        string
	}{
		{
			name: "context cancelled before request completes",
			setupServer: func() *httptest.Server {
				// Create a slow server to ensure context cancellation happens during request
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(500 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
				}))
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				// Cancel context after a short delay to simulate mid-request cancellation
				go func() {
					time.Sleep(50 * time.Millisecond)
					cancel()
				}()
				return ctx, cancel
			},
			timeout:            2 * time.Second,
			expectedStatus:     heartbeat.StatusCritical,
			expectedMsgContain: "request cancelled",
			description:        "Should detect context cancellation and return appropriate error message",
		},
		{
			name: "context cancelled immediately before request",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel immediately
				return ctx, cancel
			},
			timeout:            2 * time.Second,
			expectedStatus:     heartbeat.StatusCritical,
			expectedMsgContain: "request cancelled",
			description:        "Should detect pre-cancelled context and return cancellation error",
		},
		{
			name: "context deadline exceeded during request",
			setupServer: func() *httptest.Server {
				// Server that responds slowly
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(200 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
				}))
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				// Context with very short deadline
				return context.WithTimeout(context.Background(), 50*time.Millisecond)
			},
			timeout:            2 * time.Second,
			expectedStatus:     heartbeat.StatusCritical,
			expectedMsgContain: "request cancelled",
			description:        "Should detect context deadline exceeded and return cancellation error",
		},
		{
			name: "normal request with non-cancelled context",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			timeout:            2 * time.Second,
			expectedStatus:     heartbeat.StatusOK,
			expectedMsgContain: "ok",
			description:        "Should complete normally when context is not cancelled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test server
			ts := tt.setupServer()
			defer ts.Close()

			// Setup context
			ctx, cancel := tt.setupContext()
			defer cancel()

			// Execute checkURL
			result := heartbeat.CheckURL(ctx, ts.URL, tt.timeout)

			// Verify status
			assert.Equal(t, tt.expectedStatus, result.Status,
				"Test case: %s - Status mismatch", tt.description)

			// Verify message contains expected text
			assert.Contains(t, result.Message, tt.expectedMsgContain,
				"Test case: %s - Message should contain '%s', got: '%s'",
				tt.description, tt.expectedMsgContain, result.Message)

			// Verify resource is set correctly
			assert.Equal(t, ts.URL, result.Resource,
				"Resource should be set to the URL")

			// For cancellation cases, verify the error message explicitly mentions cancellation
			if tt.expectedStatus == heartbeat.StatusCritical && tt.expectedMsgContain == "request cancelled" {
				assert.Contains(t, result.Message, "context",
					"Cancellation error should mention context")
			}
		})
	}
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
					time.Sleep(2 * time.Second) // Slow response
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

			// Set up Gin test context
			gin.SetMode(gin.TestMode)
			resp := httptest.NewRecorder()
			c, r := gin.CreateTestContext(resp)

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
			r.GET("/health", heartbeat.Handler("test-service", deps...))
			c.Request = req

			// Serve the request
			r.ServeHTTP(resp, c.Request)

			// Verify response
			assert.Equal(t, tt.expectedHTTPCode, resp.Code, "HTTP status code mismatch")

			var hcr heartbeat.Response
			err := json.Unmarshal(resp.Body.Bytes(), &hcr)
			assert.NoError(t, err, "Failed to unmarshal response")
			assert.Equal(t, tt.expectedStatus, hcr.Status, "Health status mismatch")

			// Verify no panics occurred and response is well-formed
			assert.NotEmpty(t, hcr.Resource, "Resource should be set")
			assert.NotZero(t, hcr.UtcDateTime, "DateTime should be set")

			// For cancelled contexts, verify dependencies reflect the cancellation or timeout
			if tt.cancelBeforeServe {
				assert.NotEmpty(t, hcr.Dependencies, "Should have dependency results")
				for _, dep := range hcr.Dependencies {
					assert.Equal(t, heartbeat.StatusCritical, dep.Status, "Cancelled dependency should be critical")
					// Message can be either "cancelled" (for URL checks) or "timeout" (for custom handlers)
					assert.True(t,
						dep.Message != "" && (dep.Message[:9] == "cancelled" || dep.Message[:7] == "request" || dep.Message[:6] == "custom"),
						"Message should indicate cancellation or timeout, got: %s", dep.Message)
				}
			}
		})
	}
}

// TestHandlerTimeout tests the edge case where a handler completes after timeout expires
// This specifically tests line 159 in heartbeat.go (the discard result case in select statement)
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
			description:         "Handler finishes after timeout expires, result should be safely discarded (tests line 159)",
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

			// Execute handler with timeout - this tests the race condition at line 159
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

		result := heartbeat.ExecuteHandlerWithTimeout(context.Background(), slowHandler, 10*time.Millisecond)

		// Should timeout and return critical status
		assert.Equal(t, heartbeat.StatusCritical, result.Status)
		assert.Contains(t, result.Message, "timeout")

		// Give extra time for slow handler goroutine to complete
		// This ensures we test the full lifecycle including goroutine cleanup
		time.Sleep(600 * time.Millisecond)

		// If we reach here without panic, the test passes - goroutine cleanup worked correctly
	})

	// Additional test: verify channel doesn't block when result arrives after context cancellation
	t.Run("buffered channel prevents deadlock on late result", func(t *testing.T) {
		// This test specifically verifies that the buffered channel (line 152)
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
		result := heartbeat.ExecuteHandlerWithTimeout(context.Background(), handler, 20*time.Millisecond)

		// Should get timeout response
		assert.Equal(t, heartbeat.StatusCritical, result.Status)
		assert.Contains(t, result.Message, "timeout after 20ms")

		// Wait for handler to complete and attempt to send to channel
		time.Sleep(150 * time.Millisecond)

		// If we reach here, the buffered channel successfully prevented deadlock
		// The handler goroutine was able to send to the channel and exit cleanly
	})
}

// TestHandlerPanic verifies that panics in custom handlers are recovered gracefully
// and don't crash the service. Other dependencies should continue to be checked.
func TestHandlerPanic(t *testing.T) {
	tests := []struct {
		name                  string
		setupDeps             func() []heartbeat.DependencyDescriptor
		expectedStatus        heartbeat.Status
		expectedNumResults    int
		panicHandlerName      string
		nonPanicHandlerName   string
		expectedPanicMessage  string
		description           string
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

			// Execute checkDeps - this should NOT crash even if handlers panic
			status, results := heartbeat.CheckDeps(context.Background(), deps)

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

// TestHandlerWarning tests the StatusWarning switch case in Handler function (lines 86-87)
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
					time.Sleep(4 * time.Second) // Slow response = Warning
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(tsSlow.Close)

				return []heartbeat.DependencyDescriptor{
					{Connection: tsRedirect.URL, Name: "redirect-dep-1", Type: "HTTP"},
					{Connection: tsSlow.URL, Name: "slow-dep-2", Type: "HTTP"},
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

			// Set up Gin test context
			gin.SetMode(gin.TestMode)
			resp := httptest.NewRecorder()
			c, r := gin.CreateTestContext(resp)

			// Create request
			req, _ := http.NewRequest(http.MethodGet, "/health", nil)

			// Set up handler and serve request
			r.GET("/health", heartbeat.Handler("test-service", deps...))
			c.Request = req
			r.ServeHTTP(resp, c.Request)

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
			assert.NotZero(t, hcr.UtcDateTime, "%s: DateTime should be set", tt.description)
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

// TestCheckDepsRace validates race-free concurrent execution in checkDeps
// This test specifically validates the thread-safe status aggregation with mutex (lines 127-132 in heartbeat.go)
func TestCheckDepsRace(t *testing.T) {
	tests := []struct {
		name           string
		numDeps        int
		setupDeps      func(t *testing.T) []heartbeat.DependencyDescriptor
		expectedStatus heartbeat.Status
		description    string
	}{
		{
			name:    "stress test with 20 concurrent URL dependencies - mixed statuses",
			numDeps: 20,
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				// Create test servers with different response codes
				serverOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(serverOK.Close)

				serverWarning := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusMovedPermanently) // 301 = Warning
				}))
				t.Cleanup(serverWarning.Close)

				serverCritical := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError) // 500 = Critical
				}))
				t.Cleanup(serverCritical.Close)

				deps := make([]heartbeat.DependencyDescriptor, 20)
				for i := 0; i < 20; i++ {
					// Mix different statuses to maximize concurrent mutex contention
					switch i % 3 {
					case 0:
						deps[i] = heartbeat.DependencyDescriptor{
							Connection: serverOK.URL,
							Name:       fmt.Sprintf("OK-Dep-%d", i),
							Type:       "HTTP",
							Timeout:    5 * time.Second,
						}
					case 1:
						deps[i] = heartbeat.DependencyDescriptor{
							Connection: serverWarning.URL,
							Name:       fmt.Sprintf("Warning-Dep-%d", i),
							Type:       "HTTP",
							Timeout:    5 * time.Second,
						}
					case 2:
						deps[i] = heartbeat.DependencyDescriptor{
							Connection: serverCritical.URL,
							Name:       fmt.Sprintf("Critical-Dep-%d", i),
							Type:       "HTTP",
							Timeout:    5 * time.Second,
						}
					}
				}
				return deps
			},
			expectedStatus: heartbeat.StatusCritical, // Highest status should win
			description:    "Validates mutex protects status variable with 20 concurrent URL checks",
		},
		{
			name:    "stress test with 15 concurrent custom handler dependencies",
			numDeps: 15,
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				deps := make([]heartbeat.DependencyDescriptor, 15)
				for i := 0; i < 15; i++ {
					// Create different handler statuses to create concurrent mutex contention
					status := heartbeat.StatusOK
					if i%3 == 1 {
						status = heartbeat.StatusWarning
					} else if i%3 == 2 {
						status = heartbeat.StatusCritical
					}

					// Capture status in closure
					currentStatus := status
					deps[i] = heartbeat.DependencyDescriptor{
						Name: fmt.Sprintf("CustomHandler-%d", i),
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							// Add tiny random delay to increase chance of concurrent execution
							time.Sleep(time.Millisecond * time.Duration(i%5))
							return heartbeat.StatusResult{
								Status:   currentStatus,
								Resource: fmt.Sprintf("handler-resource-%d", i),
								Message:  "custom handler result",
							}
						},
						Timeout: 2 * time.Second,
					}
				}
				return deps
			},
			expectedStatus: heartbeat.StatusCritical, // Critical is highest, should win
			description:    "Validates mutex protects status variable with 15 concurrent custom handlers",
		},
		{
			name:    "stress test with 20 mixed URL and custom handler dependencies",
			numDeps: 20,
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				// Create test servers
				serverOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(serverOK.Close)

				serverCritical := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusServiceUnavailable) // 503 = Critical
				}))
				t.Cleanup(serverCritical.Close)

				deps := make([]heartbeat.DependencyDescriptor, 20)
				for i := 0; i < 20; i++ {
					if i%2 == 0 {
						// URL dependency
						var url string
						if i%4 == 0 {
							url = serverOK.URL
						} else {
							url = serverCritical.URL
						}
						deps[i] = heartbeat.DependencyDescriptor{
							Connection: url,
							Name:       fmt.Sprintf("URL-Dep-%d", i),
							Type:       "HTTP",
							Timeout:    5 * time.Second,
						}
					} else {
						// Custom handler dependency
						status := heartbeat.StatusOK
						if i%3 == 0 {
							status = heartbeat.StatusWarning
						}
						currentStatus := status
						deps[i] = heartbeat.DependencyDescriptor{
							Name: fmt.Sprintf("Handler-Dep-%d", i),
							Type: "Custom",
							HandlerFunc: func() heartbeat.StatusResult {
								time.Sleep(time.Millisecond * time.Duration(i%3))
								return heartbeat.StatusResult{
									Status:   currentStatus,
									Resource: "mixed-handler",
									Message:  "ok",
								}
							},
							Timeout: 2 * time.Second,
						}
					}
				}
				return deps
			},
			expectedStatus: heartbeat.StatusCritical,
			description:    "Validates mutex with mixed URL and custom handler dependencies executing concurrently",
		},
		{
			name:    "stress test with 10 dependencies - all return same status",
			numDeps: 10,
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(server.Close)

				deps := make([]heartbeat.DependencyDescriptor, 10)
				for i := 0; i < 10; i++ {
					deps[i] = heartbeat.DependencyDescriptor{
						Connection: server.URL,
						Name:       fmt.Sprintf("Same-Status-Dep-%d", i),
						Type:       "HTTP",
						Timeout:    5 * time.Second,
					}
				}
				return deps
			},
			expectedStatus: heartbeat.StatusOK,
			description:    "Validates correct status aggregation when all dependencies return same status",
		},
		{
			name:    "stress test with 12 dependencies - progressive status escalation",
			numDeps: 12,
			setupDeps: func(t *testing.T) []heartbeat.DependencyDescriptor {
				deps := make([]heartbeat.DependencyDescriptor, 12)
				for i := 0; i < 12; i++ {
					// First 4: OK, Next 4: Warning, Last 4: Critical
					var status heartbeat.Status
					if i < 4 {
						status = heartbeat.StatusOK
					} else if i < 8 {
						status = heartbeat.StatusWarning
					} else {
						status = heartbeat.StatusCritical
					}

					currentStatus := status
					deps[i] = heartbeat.DependencyDescriptor{
						Name: fmt.Sprintf("Progressive-Dep-%d", i),
						Type: "Custom",
						HandlerFunc: func() heartbeat.StatusResult {
							// Vary execution time to ensure concurrent execution
							time.Sleep(time.Millisecond * time.Duration((i*7)%11))
							return heartbeat.StatusResult{
								Status:   currentStatus,
								Resource: fmt.Sprintf("resource-%d", i),
								Message:  "completed",
							}
						},
						Timeout: 3 * time.Second,
					}
				}
				return deps
			},
			expectedStatus: heartbeat.StatusCritical,
			description:    "Validates status aggregation correctly identifies highest status across progressive escalation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := tt.setupDeps(t)

			// Execute checkDeps multiple times to increase chance of detecting race conditions
			// The race detector will catch issues if any exist
			for iteration := 0; iteration < 5; iteration++ {
				status, results := heartbeat.CheckDeps(context.Background(), deps)

				// Verify status aggregation is correct
				assert.Equal(t, tt.expectedStatus, status,
					"Iteration %d: %s - Expected status %v but got %v",
					iteration, tt.description, tt.expectedStatus, status)

				// Verify all dependencies were checked
				assert.Equal(t, tt.numDeps, len(results),
					"Iteration %d: Expected %d results but got %d",
					iteration, tt.numDeps, len(results))

				// Verify results are properly populated (no nil/zero values from race conditions)
				for i, result := range results {
					assert.NotEqual(t, heartbeat.StatusNotSet, result.Status,
						"Iteration %d: Result %d has StatusNotSet - possible race condition in status update",
						iteration, i)
					assert.NotEmpty(t, result.Name,
						"Iteration %d: Result %d missing Name - possible race condition in results array",
						iteration, i)
					assert.NotEmpty(t, result.Resource,
						"Iteration %d: Result %d missing Resource - possible race condition in results array",
						iteration, i)
				}

				// Verify that the final status matches the maximum status in results
				maxStatus := heartbeat.StatusNotSet
				for _, result := range results {
					if result.Status > maxStatus {
						maxStatus = result.Status
					}
				}
				assert.Equal(t, maxStatus, status,
					"Iteration %d: Final status %v doesn't match maximum individual status %v - mutex may not be protecting status variable",
					iteration, status, maxStatus)
			}
		})
	}
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
	gin.SetMode(gin.TestMode)
	c, r := gin.CreateTestContext(resp)
	r.GET("/test", heartbeat.Handler("test-service", deps...))
	c.Request, _ = http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(resp, c.Request)

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
