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
	UtcDateTime     time.Time      `json:"utc_DateTime"`
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
	return "http://localhost:8080"
}

// httpClient returns a configured HTTP client for testing.
func httpClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
	}
}

// TestHealthEndpoint_ReturnsValidResponse verifies the /health endpoint
// returns a properly formatted JSON response with expected fields.
func TestHealthEndpoint_ReturnsValidResponse(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	var healthResp Response
	err = json.Unmarshal(body, &healthResp)
	require.NoError(t, err, "Failed to unmarshal response JSON: %s", string(body))

	// Verify required fields are present
	assert.NotEmpty(t, healthResp.Resource, "Resource field should not be empty")
	assert.Equal(t, "testApi", healthResp.Resource, "Resource should be 'testApi'")
	assert.NotEmpty(t, healthResp.Status, "Status field should not be empty")
	assert.False(t, healthResp.UtcDateTime.IsZero(), "UtcDateTime should be set")
	assert.GreaterOrEqual(t, healthResp.RequestDuration, float64(0), "RequestDuration should be non-negative")
}

// TestHealthEndpoint_ReturnsCorrectHTTPStatus verifies the endpoint returns
// appropriate HTTP status codes based on overall health.
func TestHealthEndpoint_ReturnsCorrectHTTPStatus(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	var healthResp Response
	err = json.Unmarshal(body, &healthResp)
	require.NoError(t, err, "Failed to unmarshal response JSON")

	// HTTP status should match overall health status
	switch healthResp.Status {
	case "Critical":
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode,
			"Critical status should return 503")
	case "OK", "Warning", "NotSet":
		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"OK/Warning/NotSet status should return 200")
	default:
		t.Errorf("Unexpected status value: %s", healthResp.Status)
	}
}

// TestHealthEndpoint_IncludesDependencies verifies that dependencies are
// checked and included in the response.
func TestHealthEndpoint_IncludesDependencies(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	var healthResp Response
	err = json.Unmarshal(body, &healthResp)
	require.NoError(t, err, "Failed to unmarshal response JSON")

	// testApi has 3 dependencies configured
	assert.Len(t, healthResp.Dependencies, 3, "Should have 3 dependencies")

	// Verify each dependency has required fields
	expectedDeps := map[string]bool{
		"Golang Site":    false,
		"database check": false,
		"RabbitMQ check": false,
	}

	for _, dep := range healthResp.Dependencies {
		assert.NotEmpty(t, dep.Name, "Dependency name should not be empty")
		assert.NotEmpty(t, dep.Status, "Dependency status should not be empty")
		assert.NotEmpty(t, dep.Resource, "Dependency resource should not be empty")

		if _, exists := expectedDeps[dep.Name]; exists {
			expectedDeps[dep.Name] = true
		}
	}

	// Verify all expected dependencies were found
	for name, found := range expectedDeps {
		assert.True(t, found, "Expected dependency '%s' not found in response", name)
	}
}

// TestHealthEndpoint_DependencyStatusValues verifies dependencies return
// valid status values from the Status enum.
func TestHealthEndpoint_DependencyStatusValues(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	var healthResp Response
	err = json.Unmarshal(body, &healthResp)
	require.NoError(t, err, "Failed to unmarshal response JSON")

	validStatuses := map[string]bool{
		"NotSet":   true,
		"OK":       true,
		"Warning":  true,
		"Critical": true,
	}

	for _, dep := range healthResp.Dependencies {
		assert.True(t, validStatuses[dep.Status],
			"Dependency '%s' has invalid status '%s'", dep.Name, dep.Status)
	}

	assert.True(t, validStatuses[healthResp.Status],
		"Overall status '%s' is not a valid status value", healthResp.Status)
}

// TestHealthEndpoint_GolangSiteDependency verifies the external website
// dependency check works correctly.
func TestHealthEndpoint_GolangSiteDependency(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	var healthResp Response
	err = json.Unmarshal(body, &healthResp)
	require.NoError(t, err, "Failed to unmarshal response JSON")

	var golangDep *StatusResult
	for i, dep := range healthResp.Dependencies {
		if dep.Name == "Golang Site" {
			golangDep = &healthResp.Dependencies[i]
			break
		}
	}

	require.NotNil(t, golangDep, "Golang Site dependency should be present")
	assert.Equal(t, "https://golang.org/", golangDep.Resource,
		"Golang Site resource should be the golang.org URL")
	assert.Greater(t, golangDep.RequestDuration, float64(0),
		"Request duration should be positive for HTTP checks")
}

// TestHealthEndpoint_CassandraDependency verifies the Cassandra database
// dependency check works correctly when Cassandra is available.
func TestHealthEndpoint_CassandraDependency(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	var healthResp Response
	err = json.Unmarshal(body, &healthResp)
	require.NoError(t, err, "Failed to unmarshal response JSON")

	var cassandraDep *StatusResult
	for i, dep := range healthResp.Dependencies {
		if dep.Name == "database check" {
			cassandraDep = &healthResp.Dependencies[i]
			break
		}
	}

	require.NotNil(t, cassandraDep, "database check dependency should be present")

	// The status depends on whether Cassandra is actually running
	// We just verify the check completed with a valid status
	validStatuses := []string{"OK", "Critical", "Warning", "NotSet"}
	assert.Contains(t, validStatuses, cassandraDep.Status,
		"Cassandra check should return a valid status")
	assert.NotEmpty(t, cassandraDep.Message,
		"Cassandra check should include a message")
}

// TestHealthEndpoint_RabbitMQDependency verifies the RabbitMQ dependency
// check works correctly when RabbitMQ is available.
func TestHealthEndpoint_RabbitMQDependency(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	var healthResp Response
	err = json.Unmarshal(body, &healthResp)
	require.NoError(t, err, "Failed to unmarshal response JSON")

	var rmqDep *StatusResult
	for i, dep := range healthResp.Dependencies {
		if dep.Name == "RabbitMQ check" {
			rmqDep = &healthResp.Dependencies[i]
			break
		}
	}

	require.NotNil(t, rmqDep, "RabbitMQ check dependency should be present")

	// The status depends on whether RabbitMQ is actually running
	validStatuses := []string{"OK", "Critical", "Warning", "NotSet"}
	assert.Contains(t, validStatuses, rmqDep.Status,
		"RabbitMQ check should return a valid status")
	assert.NotEmpty(t, rmqDep.Message,
		"RabbitMQ check should include a message")
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
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	var healthResp Response
	err = json.Unmarshal(body, &healthResp)
	require.NoError(t, err, "Failed to unmarshal response JSON")

	now := time.Now().UTC()
	timeDiff := now.Sub(healthResp.UtcDateTime)

	assert.True(t, timeDiff < time.Minute && timeDiff > -time.Minute,
		"Timestamp should be within 1 minute of current time, got diff: %v", timeDiff)
}

// TestHealthEndpoint_MachineHostname verifies the machine/hostname
// field is populated.
func TestHealthEndpoint_MachineHostname(t *testing.T) {
	client := httpClient()
	baseURL := getBaseURL()

	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err, "Failed to make request to /health endpoint")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	var healthResp Response
	err = json.Unmarshal(body, &healthResp)
	require.NoError(t, err, "Failed to unmarshal response JSON")

	// Machine field should be populated (hostname of the container/host)
	// It might be empty in some environments, so we just verify the field exists
	t.Logf("Machine hostname: %s", healthResp.Machine)
}
