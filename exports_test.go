package heartbeat

import (
	"context"
	"time"
)

// CheckDeps wraps checkDeps for testing with a background context
func CheckDeps(deps []DependencyDescriptor) (Status, []StatusResult) {
	return checkDeps(context.Background(), deps)
}

// CheckURL wraps checkURL for testing with a default timeout and background context
func CheckURL(urlStr string) StatusResult {
	return checkURL(context.Background(), urlStr, 10*time.Second)
}
