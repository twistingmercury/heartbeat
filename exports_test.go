package heartbeat

import "time"

var (
	CheckDeps = checkDeps
)

// CheckURL wraps checkURL for testing with a default timeout
func CheckURL(urlStr string) StatusResult {
	return checkURL(urlStr, 10*time.Second)
}
