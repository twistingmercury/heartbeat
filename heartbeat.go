package heartbeat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

		hb := Response{
			Resource:    svcName,
			UtcDateTime: time.Now().UTC(),
		}
		status, checkedDeps := checkDeps(deps)
		hb.Dependencies = checkedDeps
		hb.Status = status

		hb.RequestDuration = float64(time.Since(st).Microseconds()) / 1000

		httpStatus := http.StatusOK
		if hb.Status == StatusCritical {
			httpStatus = http.StatusServiceUnavailable // 503
		} else if hb.Status == StatusWarning {
			httpStatus = http.StatusOK // 200 - still operational but degraded
		}
		c.JSON(httpStatus, hb)
	}
}

func checkDeps(deps []DependencyDescriptor) (status Status, hbl []StatusResult) {
	for _, desc := range deps {
		hsr := StatusResult{Status: StatusOK}
		switch {
		case desc.HandlerFunc != nil:
			hsr = desc.HandlerFunc()
		default:
			hsr = checkURL(desc.Connection, desc.Timeout)
		}
		if hsr.Status > status {
			status = hsr.Status
		}
		hsr.Name = desc.Name
		hbl = append(hbl, hsr)
	}
	return
}

func checkURL(urlStr string, timeout time.Duration) StatusResult {
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

	// Make HTTP request
	r, err := client.Get(urlStr)
	elapsed := time.Since(st)
	hsr.RequestDuration = float64(elapsed.Microseconds()) / 1000

	if err != nil {
		hsr.Status = StatusCritical
		hsr.Message = fmt.Sprintf("HTTP request failed: %v", err)
		return hsr
	}

	defer r.Body.Close()
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
