package heartbeat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	defaultTimeout       = 10 * time.Second
	defaultSlowThreshold = 3 * time.Second
	maxDrainBytes        = 64 << 10
)

// httpClient is shared so dependency checks reuse connections. Per-check
// timeouts are applied through the request context.
var httpClient = &http.Client{}

// StatusHandlerFunc is a function that returns the status of a resource.
//
// Deprecated: use CheckFunc. A StatusHandlerFunc cannot be cancelled, so a
// check that hangs keeps its goroutine alive after the timeout expires.
type StatusHandlerFunc func() (status StatusResult)

// CheckFunc returns the status of a resource. The context is cancelled when
// the dependency's timeout expires or the caller goes away; the check must
// return promptly once that happens.
type CheckFunc func(ctx context.Context) StatusResult

// DependencyDescriptor defines a resource to be checked during a heartbeat request.
//
// Exactly one check is used, in this order of precedence: CheckFunc,
// HandlerFunc, then an HTTP GET of Connection.
type DependencyDescriptor struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Connection  string            `json:"connection"`
	HandlerFunc StatusHandlerFunc `json:"-"`
	CheckFunc   CheckFunc         `json:"-"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	// SlowThreshold marks a successful HTTP check as Warning when it takes
	// longer than this. It defaults to three seconds.
	SlowThreshold time.Duration `json:"slow_threshold,omitempty"`
}

func (d *DependencyDescriptor) String() string {
	redacted := *d
	redacted.Connection = redactURL(d.Connection)
	text, _ := json.MarshalIndent(redacted, "", "  ")
	return string(text)
}

func (d *DependencyDescriptor) validate() error {
	if d.Timeout < 0 {
		return errors.New("timeout must not be negative")
	}
	if d.SlowThreshold < 0 {
		return errors.New("slow threshold must not be negative")
	}
	if d.CheckFunc != nil || d.HandlerFunc != nil {
		return nil
	}
	if d.Connection == "" {
		return errors.New("needs a CheckFunc, a HandlerFunc or a Connection URL")
	}
	_, err := parseCheckURL(d.Connection)
	return err
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
//
// It panics if a descriptor cannot be checked (no check and no Connection,
// an invalid or non-HTTP URL, or a negative duration), so configuration
// mistakes surface at startup instead of as a permanent Critical status.
//
// Concurrent requests share one run of the dependency checks, so a burst of
// probes does not multiply the load on the dependencies.
func Handler(svcName string, deps ...DependencyDescriptor) gin.HandlerFunc {
	deps = slices.Clone(deps)
	for i := range deps {
		if err := deps[i].validate(); err != nil {
			panic(fmt.Sprintf("heartbeat: dependency %d (%q): %v", i, deps[i].Name, err))
		}
	}

	hostname, _ := os.Hostname()
	var checks flightGroup

	return func(c *gin.Context) {
		st := time.Now()

		status, results, err := checks.do(c.Request.Context(), deps)
		if err != nil {
			// The caller went away before the checks finished.
			c.Status(http.StatusServiceUnavailable)
			return
		}

		hb := Response{
			Status:          status,
			Name:            svcName,
			Resource:        svcName,
			Machine:         hostname,
			UtcDateTime:     st.UTC(),
			RequestDuration: durationMs(time.Since(st)),
			Dependencies:    results,
		}

		httpStatus := http.StatusOK
		if status != StatusOK && status != StatusWarning {
			httpStatus = http.StatusServiceUnavailable
		}
		c.JSON(httpStatus, hb)
	}
}

// CheckDependencies runs every dependency check concurrently and returns the
// most severe status together with the results in declaration order. With no
// dependencies the status is OK. A check that returns a status other than OK,
// Warning or Critical is reported as Critical.
func CheckDependencies(ctx context.Context, deps []DependencyDescriptor) (Status, []StatusResult) {
	results := make([]StatusResult, len(deps))

	var wg sync.WaitGroup
	for i, d := range deps {
		wg.Go(func() {
			results[i] = checkDependency(ctx, d)
		})
	}
	wg.Wait()

	status := StatusOK
	for _, r := range results {
		status = max(status, r.Status)
	}
	return status, results
}

func checkDependency(ctx context.Context, d DependencyDescriptor) StatusResult {
	var hsr StatusResult
	switch {
	case d.CheckFunc != nil:
		hsr = runCheck(ctx, d.CheckFunc, d.Timeout)
	case d.HandlerFunc != nil:
		handler := d.HandlerFunc
		hsr = runCheck(ctx, func(context.Context) StatusResult { return handler() }, d.Timeout)
	default:
		hsr = checkURL(ctx, d.Connection, d.Timeout, d.SlowThreshold)
	}

	hsr.Name = d.Name
	if hsr.Resource == "" {
		hsr.Resource = d.Name
	}

	switch hsr.Status {
	case StatusOK, StatusWarning, StatusCritical:
	default:
		msg := "check returned no status"
		if hsr.Status != StatusNotSet {
			msg = fmt.Sprintf("check returned invalid status %d", int(hsr.Status))
		}
		if hsr.Message != "" {
			msg += ": " + hsr.Message
		}
		hsr.Status = StatusCritical
		hsr.Message = msg
	}
	return hsr
}

// runCheck runs a custom check with timeout enforcement and panic recovery.
func runCheck(parent context.Context, check CheckFunc, timeout time.Duration) StatusResult {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	st := time.Now()
	// Buffered so the check goroutine can always deliver and exit, even after
	// runCheck has stopped waiting.
	resultChan := make(chan StatusResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// The panic value can hold connection strings, so it is logged
				// rather than returned to the caller.
				slog.Error("heartbeat: dependency check panicked", "panic", r)
				resultChan <- StatusResult{
					Status:  StatusCritical,
					Message: "panic in custom handler",
				}
			}
		}()
		resultChan <- check(ctx)
	}()

	select {
	case result := <-resultChan:
		if result.RequestDuration == 0 {
			result.RequestDuration = durationMs(time.Since(st))
		}
		return result
	case <-ctx.Done():
		result := StatusResult{
			Status:          StatusCritical,
			RequestDuration: durationMs(time.Since(st)),
			Message:         fmt.Sprintf("custom handler timeout after %v", timeout),
		}
		if err := parent.Err(); err != nil {
			result.Message = fmt.Sprintf("check cancelled: %v", err)
		}
		return result
	}
}

func checkURL(parent context.Context, urlStr string, timeout, slowThreshold time.Duration) StatusResult {
	u, err := parseCheckURL(urlStr)
	if err != nil {
		return StatusResult{Status: StatusCritical, Message: err.Error()}
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if slowThreshold <= 0 {
		slowThreshold = defaultSlowThreshold
	}

	hsr := StatusResult{Resource: u.Redacted()}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		hsr.Status = StatusCritical
		hsr.Message = "failed to create request"
		return hsr
	}

	st := time.Now()
	r, err := httpClient.Do(req)
	elapsed := time.Since(st)
	hsr.RequestDuration = durationMs(elapsed)

	if err != nil {
		hsr.Status = StatusCritical
		hsr.Message = describeRequestError(parent, err, timeout)
		return hsr
	}
	defer func() {
		// Draining lets the transport reuse the connection.
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxDrainBytes))
		_ = r.Body.Close()
	}()
	hsr.StatusCode = r.StatusCode

	switch {
	case r.StatusCode >= 500:
		hsr.Status = StatusCritical
		hsr.Message = fmt.Sprintf("server error (HTTP %d)", r.StatusCode)
	case r.StatusCode >= 400:
		hsr.Status = StatusCritical
		hsr.Message = fmt.Sprintf("client error (HTTP %d)", r.StatusCode)
	case r.StatusCode >= 300:
		// Redirects with a Location header are followed, so this is a 3xx
		// the client could not follow.
		hsr.Status = StatusWarning
		hsr.Message = fmt.Sprintf("redirect (HTTP %d)", r.StatusCode)
	case elapsed > slowThreshold:
		hsr.Status = StatusWarning
		hsr.Message = fmt.Sprintf("slow response (%v)", elapsed)
	default:
		hsr.Status = StatusOK
		hsr.Message = "ok"
	}
	return hsr
}

// parseCheckURL accepts absolute http and https URLs. Its errors never
// contain the URL, which may carry credentials.
func parseCheckURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("invalid URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme: %q (only http/https allowed)", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("invalid URL: missing host")
	}
	return u, nil
}

// describeRequestError reports why a request failed without echoing the
// addresses that transport errors embed.
func describeRequestError(parent context.Context, err error, timeout time.Duration) string {
	if perr := parent.Err(); perr != nil {
		return fmt.Sprintf("request cancelled: %v", perr)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("HTTP request failed: timeout after %v", timeout)
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		return fmt.Sprintf("HTTP request failed: %s: %v", opErr.Op, opErr.Err)
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "HTTP request failed: DNS lookup failed"
	}
	return "HTTP request failed"
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Redacted()
}

func durationMs(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

// flightGroup lets concurrent callers share one run of the dependency checks.
type flightGroup struct {
	mu  sync.Mutex
	cur *flight
}

type flight struct {
	done    chan struct{}
	status  Status
	results []StatusResult
}

func (g *flightGroup) do(ctx context.Context, deps []DependencyDescriptor) (Status, []StatusResult, error) {
	g.mu.Lock()
	f := g.cur
	if f == nil {
		f = &flight{done: make(chan struct{})}
		g.cur = f
		// The run is shared, so one caller disconnecting must not cancel it
		// for the others. Each check is still bounded by its own timeout.
		runCtx := context.WithoutCancel(ctx)
		go func() {
			f.status, f.results = CheckDependencies(runCtx, deps)
			g.mu.Lock()
			g.cur = nil
			g.mu.Unlock()
			close(f.done)
		}()
	}
	g.mu.Unlock()

	select {
	case <-f.done:
		return f.status, f.results, nil
	case <-ctx.Done():
		return StatusNotSet, nil, ctx.Err()
	}
}
