// Package e2e provides end-to-end tests for the heartbeat testApi service.
// These tests validate the health check endpoint behavior from an external
// consumer's perspective, treating the service as a black box.
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Response represents the heartbeat health check response.
// This is a local copy to maintain black-box testing principles.
type Response struct {
	Status          string         `json:"status"`
	Name            string         `json:"name,omitempty"`
	Resource        string         `json:"resource"`
	Machine         string         `json:"machine,omitempty"`
	UTCDateTime     time.Time      `json:"utc_date_time"`
	RequestDuration float64        `json:"request_duration_ms"`
	Message         string         `json:"message,omitempty"`
	Dependencies    []StatusResult `json:"dependencies,omitempty"`
}

// StatusResult represents the status of a dependency check.
type StatusResult struct {
	Status          string  `json:"status"`
	Name            string  `json:"name,omitempty"`
	Resource        string  `json:"resource"`
	RequestDuration float64 `json:"request_duration_ms"`
	StatusCode      int     `json:"http_status_code"`
	Message         string  `json:"message,omitempty"`
}

// getBaseURL returns the base URL for the testApi service.
func getBaseURL() string {
	if url := os.Getenv("TESTAPI_URL"); url != "" {
		return url
	}
	return "http://localhost:9080"
}

// httpClient returns a configured HTTP client for testing.
func httpClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
	}
}

// getHealth fetches /health once and decodes the response body.
func getHealth(t *testing.T) (*http.Response, Response) {
	t.Helper()

	resp, err := httpClient().Get(getBaseURL() + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	var healthResp Response
	require.NoError(t, json.Unmarshal(body, &healthResp),
		"Failed to unmarshal response JSON: %s", string(body))
	return resp, healthResp
}

// findDependency returns the named dependency, failing the test if absent.
func findDependency(t *testing.T, healthResp Response, name string) StatusResult {
	t.Helper()
	for _, dep := range healthResp.Dependencies {
		if dep.Name == name {
			return dep
		}
	}
	require.FailNow(t, "dependency not found", "Expected dependency %q in response: %+v",
		name, healthResp.Dependencies)
	return StatusResult{}
}

// TestHealthEndpoint_ReturnsValidResponse verifies the /health endpoint
// returns a properly formatted JSON response with expected fields.
func TestHealthEndpoint_ReturnsValidResponse(t *testing.T) {
	_, healthResp := getHealth(t)

	assert.Equal(t, "testApi", healthResp.Resource, "Resource should be 'testApi'")
	assert.False(t, healthResp.UTCDateTime.IsZero(), "UTCDateTime should be set")
	assert.GreaterOrEqual(t, healthResp.RequestDuration, float64(0), "RequestDuration should be non-negative")
}

// TestHealthEndpoint_OverallStatusOK verifies that, with every dependency
// healthy (the compose stack is health-gated), the overall status is OK and
// the endpoint returns HTTP 200.
func TestHealthEndpoint_OverallStatusOK(t *testing.T) {
	resp, healthResp := getHealth(t)

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Healthy service should return 200")
	assert.Equal(t, "OK", healthResp.Status,
		"Overall status should be OK; dependencies: %+v", healthResp.Dependencies)
}

// TestHealthEndpoint_IncludesDependencies verifies that every configured
// dependency is checked, included in the response, and healthy.
func TestHealthEndpoint_IncludesDependencies(t *testing.T) {
	_, healthResp := getHealth(t)

	require.Len(t, healthResp.Dependencies, 3, "Should have 3 dependencies")

	for _, name := range []string{"RabbitMQ management UI", "database check", "RabbitMQ check"} {
		dep := findDependency(t, healthResp, name)
		assert.NotEmpty(t, dep.Resource, "Dependency %q resource should not be empty", name)
		assert.Equal(t, "OK", dep.Status, "Dependency %q should be OK: %s", name, dep.Message)
	}
}

// TestHealthEndpoint_HTTPDependency verifies the plain HTTP dependency check
// against RabbitMQ's management UI.
func TestHealthEndpoint_HTTPDependency(t *testing.T) {
	_, healthResp := getHealth(t)

	dep := findDependency(t, healthResp, "RabbitMQ management UI")
	assert.Equal(t, "http://rabbitmq:15672/", dep.Resource,
		"HTTP dependency resource should be the management UI URL")
	assert.Equal(t, "OK", dep.Status, "HTTP dependency should be OK: %s", dep.Message)
	assert.Greater(t, dep.RequestDuration, float64(0),
		"Request duration should be positive for HTTP checks")
}

// TestHealthEndpoint_CassandraDependency verifies the Cassandra database
// dependency check reports OK.
func TestHealthEndpoint_CassandraDependency(t *testing.T) {
	_, healthResp := getHealth(t)

	dep := findDependency(t, healthResp, "database check")
	assert.Equal(t, "OK", dep.Status, "Cassandra check should be OK: %s", dep.Message)
	assert.NotEmpty(t, dep.Message, "Cassandra check should include a message")
}

// TestHealthEndpoint_RabbitMQDependency verifies the RabbitMQ aliveness
// dependency check reports OK.
func TestHealthEndpoint_RabbitMQDependency(t *testing.T) {
	_, healthResp := getHealth(t)

	dep := findDependency(t, healthResp, "RabbitMQ check")
	assert.Equal(t, "OK", dep.Status, "RabbitMQ check should be OK: %s", dep.Message)
	assert.NotEmpty(t, dep.Message, "RabbitMQ check should include a message")
}

// TestHealthEndpoint_MethodNotAllowed verifies that non-GET methods
// return appropriate error responses.
func TestHealthEndpoint_MethodNotAllowed(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	methods := []string{"POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req, err := http.NewRequest(method, baseURL+"/health", nil)
			require.NoError(t, err, "Failed to create request")

			resp, err := client.Do(req)
			require.NoError(t, err, "Failed to execute request")
			defer resp.Body.Close()

			// Gin returns 404 for method not allowed by default
			assert.Equal(t, http.StatusNotFound, resp.StatusCode,
				"%s method should return 404", method)
		})
	}
}

// TestHealthEndpoint_NonExistentPath verifies 404 is returned for
// unknown paths.
func TestHealthEndpoint_NonExistentPath(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/nonexistent")
	require.NoError(t, err, "Failed to make request")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode,
		"Non-existent path should return 404")
}

// TestHealthEndpoint_ResponseHeaders verifies correct content-type
// and other headers are returned.
func TestHealthEndpoint_ResponseHeaders(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	assert.Contains(t, contentType, "application/json",
		"Content-Type should be application/json")
}

// TestHealthEndpoint_ConcurrentRequests verifies the endpoint handles
// multiple concurrent requests correctly.
func TestHealthEndpoint_ConcurrentRequests(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	const numRequests = 10
	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			resp, err := client.Get(baseURL + "/health")
			if err != nil {
				results <- err
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				results <- err
				return
			}

			var healthResp Response
			if err := json.Unmarshal(body, &healthResp); err != nil {
				results <- fmt.Errorf("invalid JSON response: %w", err)
				return
			}

			if resp.StatusCode != http.StatusOK || healthResp.Status != "OK" {
				results <- fmt.Errorf("unhealthy response: HTTP %d, status %q", resp.StatusCode, healthResp.Status)
				return
			}

			if healthResp.Resource != "testApi" {
				results <- fmt.Errorf("unexpected resource: %s", healthResp.Resource)
				return
			}

			results <- nil
		}()
	}

	for i := 0; i < numRequests; i++ {
		err := <-results
		assert.NoError(t, err, "Concurrent request %d failed", i+1)
	}
}

// TestHealthEndpoint_TimestampIsRecent verifies the returned timestamp
// is recent (within the last minute).
func TestHealthEndpoint_TimestampIsRecent(t *testing.T) {
	_, healthResp := getHealth(t)

	now := time.Now().UTC()
	timeDiff := now.Sub(healthResp.UTCDateTime)

	assert.True(t, timeDiff < time.Minute && timeDiff > -time.Minute,
		"Timestamp should be within 1 minute of current time, got diff: %v", timeDiff)
}

// TestHealthEndpoint_MachineHostname verifies the machine/hostname
// field is populated (the testApi container always has a hostname).
func TestHealthEndpoint_MachineHostname(t *testing.T) {
	_, healthResp := getHealth(t)

	assert.NotEmpty(t, healthResp.Machine, "Machine hostname should be populated")
}
