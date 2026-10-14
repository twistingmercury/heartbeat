package heartbeat

import (
	"context"
	"time"
)

// CheckURL is exported for testing. It uses the default slow threshold.
func CheckURL(ctx context.Context, urlStr string, timeout time.Duration) StatusResult {
	return checkURL(ctx, urlStr, timeout, 0)
}

// CheckURLWithSlowThreshold is exported for testing.
var CheckURLWithSlowThreshold = checkURL

// ExecuteHandlerWithTimeout is exported for testing.
func ExecuteHandlerWithTimeout(ctx context.Context, handler StatusHandlerFunc, timeout time.Duration) StatusResult {
	return runCheck(ctx, func(context.Context) StatusResult { return handler() }, timeout)
}

// RunCheck is exported for testing.
var RunCheck = runCheck
