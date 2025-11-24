package heartbeat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// StatusHandlerFunc is a function that returns the status of a resource.
type StatusHandlerFunc func() (status StatusResult)

// DependencyDescriptor defines a resource to be checked during a heartbeat request.
type DependencyDescriptor struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Connection  string            `json:"connection"`
	HandlerFunc StatusHandlerFunc `json:"-"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
}

func (d *DependencyDescriptor) String() string {
	text, _ := json.MarshalIndent(d, "", "  ")
	return string(text)
}

// StatusResult represents another process or API that this service relies upon to be considered healthy.
type StatusResult struct {
	Status          Status  `json:"status"`
	Name            string  `json:"name,omitempty"`
	Resource        string  `json:"resource"`
	RequestDuration float64 `json:"request_duration_ms"`
	StatusCode      int     `json:"http_status_code"`
	Message         string  `json:"message,omitempty"`
}

func (dep *StatusResult) String() string {
	text, _ := json.MarshalIndent(dep, "", "  ")
	return string(text)
}

// Response is the response to be returned to the caller.
type Response struct {
	Status          Status         `json:"status"`
	Name            string         `json:"name,omitempty"`
	Resource        string         `json:"resource"`
	Machine         string         `json:"machine,omitempty"`
	UtcDateTime     time.Time      `json:"utc_DateTime"`
	RequestDuration float64        `json:"request_duration_ms"`
	Message         string         `json:"message,omitempty"`
	Dependencies    []StatusResult `json:"dependencies,omitempty"`
}

func (h *Response) String() string {
	text, _ := json.Marshal(h)
	return string(text)
}

// Handler returns the health of the app as a Response object.
func Handler(svcName string, deps ...DependencyDescriptor) gin.HandlerFunc {
	return func(c *gin.Context) {
		st := time.Now()

		// Get hostname; use empty string as fallback if unavailable
		hostname, err := os.Hostname()
		if err != nil {
			hostname = ""
		}

		hb := Response{
			Name:        svcName,
			Resource:    svcName,
			Machine:     hostname,
			UtcDateTime: time.Now().UTC(),
		}

		// Get context from request for cancellation and deadline propagation
		ctx := c.Request.Context()
		status, checkedDeps := checkDeps(ctx, deps)
		hb.Dependencies = checkedDeps
		hb.Status = status

		hb.RequestDuration = float64(time.Since(st).Microseconds()) / 1000

		httpStatus := http.StatusOK
		switch hb.Status {
		case StatusCritical:
			httpStatus = http.StatusServiceUnavailable // 503
		case StatusWarning:
			httpStatus = http.StatusOK // 200 - still operational but degraded
		case StatusOK, StatusNotSet:
			httpStatus = http.StatusOK // 200 - healthy or no dependencies checked
		}
		c.JSON(httpStatus, hb)
	}
}

func checkDeps(ctx context.Context, deps []DependencyDescriptor) (status Status, hbl []StatusResult) {
	// Pre-allocate results slice with known length
	results := make([]StatusResult, len(deps))

	// Use WaitGroup for concurrent dependency checking
	var wg sync.WaitGroup
	var mu sync.Mutex // Protect status variable

	for i, desc := range deps {
		wg.Add(1)
		go func(index int, d DependencyDescriptor) {
			defer wg.Done()

			var hsr StatusResult

			switch {
			case d.HandlerFunc != nil:
				// Wrap custom handler with timeout enforcement
				hsr = executeHandlerWithTimeout(ctx, d.HandlerFunc, d.Timeout)
			default:
				hsr = checkURL(ctx, d.Connection, d.Timeout)
			}

			// Set name from descriptor
			hsr.Name = d.Name

			// Fix Issue #1: Set Resource field for custom handlers if empty
			if hsr.Resource == "" {
				hsr.Resource = d.Name
			}

			// Thread-safe status update
			mu.Lock()
			if hsr.Status > status {
				status = hsr.Status
			}
			results[index] = hsr
			mu.Unlock()
		}(i, desc)
	}

	wg.Wait()
	return status, results
}

// executeHandlerWithTimeout wraps custom handler execution with timeout enforcement
func executeHandlerWithTimeout(ctx context.Context, handler StatusHandlerFunc, timeout time.Duration) StatusResult {
	// Default timeout for custom handlers
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Channel to receive result
	resultChan := make(chan StatusResult, 1)

	// Execute handler in goroutine with panic recovery
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Panic occurred in handler - convert to critical status result
				panicResult := StatusResult{
					Status:  StatusCritical,
					Message: fmt.Sprintf("panic in custom handler: %v", r),
				}
				select {
				case resultChan <- panicResult:
				case <-timeoutCtx.Done():
					// Context already timed out, discard panic result
				}
			}
		}()

		result := handler()
		select {
		case resultChan <- result:
		case <-timeoutCtx.Done():
			// Handler finished but context already timed out, discard result
		}
	}()

	// Wait for result or timeout
	select {
	case result := <-resultChan:
		return result
	case <-timeoutCtx.Done():
		// Timeout or cancellation occurred
		return StatusResult{
			Status:  StatusCritical,
			Message: fmt.Sprintf("custom handler timeout after %v", timeout),
		}
	}
}

func checkURL(ctx context.Context, urlStr string, timeout time.Duration) StatusResult {
	var hsr StatusResult
	st := time.Now()

	// Validate URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		hsr.Status = StatusCritical
		hsr.Message = fmt.Sprintf("invalid URL: %v", err)
		hsr.Resource = urlStr
		hsr.Name = urlStr
		return hsr
	}

	// Only allow HTTP and HTTPS schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		hsr.Status = StatusCritical
		hsr.Message = fmt.Sprintf("unsupported URL scheme: %s (only http/https allowed)", parsedURL.Scheme)
		hsr.Resource = urlStr
		hsr.Name = urlStr
		return hsr
	}

	hsr.Name = urlStr
	hsr.Resource = urlStr
	hsr.Status = StatusNotSet

	// Set timeout with default
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: timeout,
	}

	// Create request with context for cancellation support
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		hsr.Status = StatusCritical
		hsr.Message = fmt.Sprintf("failed to create request: %v", err)
		return hsr
	}

	// Make HTTP request
	r, err := client.Do(req)
	elapsed := time.Since(st)
	hsr.RequestDuration = float64(elapsed.Microseconds()) / 1000

	if err != nil {
		hsr.Status = StatusCritical
		// Check if error is due to context cancellation
		if ctx.Err() != nil {
			hsr.Message = fmt.Sprintf("request cancelled: %v", ctx.Err())
		} else {
			hsr.Message = fmt.Sprintf("HTTP request failed: %v", err)
		}
		return hsr
	}

	defer func() {
		_ = r.Body.Close() // Error intentionally ignored - cleanup operation after successful request
	}()
	hsr.StatusCode = r.StatusCode

	// Evaluate status based on HTTP status code and response time
	switch {
	case r.StatusCode >= 500:
		hsr.Status = StatusCritical
		hsr.Message = fmt.Sprintf("server error (HTTP %d)", r.StatusCode)
	case r.StatusCode >= 400:
		hsr.Status = StatusCritical
		hsr.Message = fmt.Sprintf("client error (HTTP %d)", r.StatusCode)
	case r.StatusCode >= 300:
		hsr.Status = StatusWarning
		hsr.Message = fmt.Sprintf("redirect (HTTP %d)", r.StatusCode)
	case r.StatusCode >= 200:
		if elapsed > 3*time.Second {
			hsr.Status = StatusWarning
			hsr.Message = fmt.Sprintf("slow response (%v)", elapsed)
		} else {
			hsr.Status = StatusOK
			hsr.Message = "ok"
		}
	default:
		hsr.Status = StatusCritical
		hsr.Message = fmt.Sprintf("unexpected status (HTTP %d)", r.StatusCode)
	}

	return hsr
}
