package heartbeat_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/heartbeat/v2"
)

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

	act := heartbeat.CheckURLWithSlowThreshold(context.Background(), ts.URL, 10*time.Second, slowThreshold)
	assert.Equal(t, heartbeat.StatusWarning, act.Status)
}

func TestCheckUrlReturnCritical(t *testing.T) {
	ts := testServer(500, false)
	defer ts.Close()

	act := heartbeat.CheckURL(context.Background(), ts.URL, 10*time.Second)
	assert.Equal(t, heartbeat.StatusCritical, act.Status)
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
