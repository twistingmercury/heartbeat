package heartbeat_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/heartbeat/v2"
)

func TestCheckDependenciesInvokesHandlerFunc(t *testing.T) {
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

	s, r := heartbeat.CheckDependencies(context.Background(), deps)
	assert.Equal(t, heartbeat.StatusOK, s)
	assert.Equal(t, 2, len(r))
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

// TestCheckDependenciesRace validates race-free concurrent execution in CheckDependencies
// This test specifically validates the thread-safe status aggregation with mutex
func TestCheckDependenciesRace(t *testing.T) {
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
					var status heartbeat.Status
					switch i % 3 {
					case 1:
						status = heartbeat.StatusWarning
					case 2:
						status = heartbeat.StatusCritical
					default:
						status = heartbeat.StatusOK
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

			// Execute CheckDependencies multiple times to increase chance of detecting race conditions
			// The race detector will catch issues if any exist
			for iteration := 0; iteration < 5; iteration++ {
				status, results := heartbeat.CheckDependencies(context.Background(), deps)

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

func TestCheckDependenciesDoesNotEchoURL(t *testing.T) {
	for _, tt := range leakyURLs {
		t.Run(tt.name, func(t *testing.T) {
			deps := []heartbeat.DependencyDescriptor{{Name: "db", Connection: tt.url}}

			status, results := heartbeat.CheckDependencies(context.Background(), deps)

			assert.Equal(t, heartbeat.StatusCritical, status)
			require.Len(t, results, 1)
			assert.Equal(t, heartbeat.StatusCritical, results[0].Status)
			assert.NotContains(t, results[0].Message, tt.secret)
			assert.NotContains(t, results[0].Message, "hunter2")
		})
	}
}
