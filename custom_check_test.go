package heartbeat_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/twistingmercury/heartbeat/v2"
)

func TestDependencyHandlerFunc(t *testing.T) {
	exp := heartbeat.StatusResult{
		Status:          heartbeat.StatusOK,
		Name:            "Test Func",
		RequestDuration: 42,
		Resource:        "None",
	}
	dep := heartbeat.DependencyDescriptor{
		HandlerFunc: func() (hsr heartbeat.StatusResult) {
			return exp
		},
	}
	act := dep.HandlerFunc()

	assert.Equal(t, exp, act)
}

func TestExecuteHandlerWithTimeoutContextCancellation(t *testing.T) {
	tests := []struct {
		name                string
		setupHandler        func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc)
		timeout             time.Duration
		expectedStatus      heartbeat.Status
		expectedMessagePart string
		description         string
	}{
		{
			name: "parent context cancelled before handler completes",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				handler := func() heartbeat.StatusResult {
					// Simulate long-running handler
					time.Sleep(200 * time.Millisecond)
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed successfully",
					}
				}
				return ctx, cancel, handler
			},
			timeout:             500 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "check cancelled",
			description:         "When parent context is cancelled, should return critical status with cancellation message",
		},
		{
			name: "timeout expires before handler completes",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx := context.Background()
				handler := func() heartbeat.StatusResult {
					// Handler takes longer than timeout
					time.Sleep(300 * time.Millisecond)
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed successfully",
					}
				}
				return ctx, nil, handler
			},
			timeout:             50 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "custom handler timeout after 50ms",
			description:         "When timeout expires before handler completes, should return critical status with timeout message",
		},
		{
			name: "handler completes before timeout",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx := context.Background()
				handler := func() heartbeat.StatusResult {
					// Fast handler that completes quickly
					time.Sleep(10 * time.Millisecond)
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed successfully",
					}
				}
				return ctx, nil, handler
			},
			timeout:             500 * time.Millisecond,
			expectedStatus:      heartbeat.StatusOK,
			expectedMessagePart: "completed successfully",
			description:         "When handler completes before timeout, should return handler's actual result",
		},
		{
			name: "race condition - result arrives after timeout",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx := context.Background()
				handler := func() heartbeat.StatusResult {
					// Handler completes just after timeout expires
					// This tests the race condition where the result
					// tries to be sent to resultChan but context is already done
					time.Sleep(100 * time.Millisecond)
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed after timeout",
					}
				}
				return ctx, nil, handler
			},
			timeout:             50 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "custom handler timeout after 50ms",
			description:         "When handler completes after timeout, result should be discarded and timeout returned",
		},
		{
			name: "default timeout applied when zero",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx := context.Background()
				handler := func() heartbeat.StatusResult {
					// Handler completes quickly
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed successfully",
					}
				}
				return ctx, nil, handler
			},
			timeout:             0, // Should use default 10s timeout
			expectedStatus:      heartbeat.StatusOK,
			expectedMessagePart: "completed successfully",
			description:         "When timeout is zero, should use default 10s timeout and complete successfully",
		},
		{
			name: "immediate context cancellation",
			setupHandler: func() (context.Context, context.CancelFunc, heartbeat.StatusHandlerFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				handler := func() heartbeat.StatusResult {
					// Handler that would complete successfully if given time
					time.Sleep(50 * time.Millisecond)
					return heartbeat.StatusResult{
						Status:   heartbeat.StatusOK,
						Resource: "test-resource",
						Message:  "completed successfully",
					}
				}
				return ctx, cancel, handler
			},
			timeout:             500 * time.Millisecond,
			expectedStatus:      heartbeat.StatusCritical,
			expectedMessagePart: "check cancelled",
			description:         "When context is cancelled immediately, should report cancellation rather than a timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel, handler := tt.setupHandler()

			// For tests that require cancellation, cancel the context with a slight delay
			// to ensure the handler goroutine has started
			if cancel != nil && (tt.name == "parent context cancelled before handler completes" || tt.name == "immediate context cancellation") {
				// Start a goroutine to cancel context
				go func() {
					if tt.name == "immediate context cancellation" {
						// Cancel immediately
						cancel()
					} else {
						// Cancel after a short delay to simulate mid-execution cancellation
						time.Sleep(50 * time.Millisecond)
						cancel()
					}
				}()
				defer cancel() // Ensure cleanup even if test fails
			} else if cancel != nil {
				defer cancel()
			}

			// Execute the handler with timeout
			result := heartbeat.ExecuteHandlerWithTimeout(ctx, handler, tt.timeout)

			// Verify status
			assert.Equal(t, tt.expectedStatus, result.Status,
				"Test case: %s - Status mismatch", tt.description)

			// Verify message contains expected part
			assert.Contains(t, result.Message, tt.expectedMessagePart,
				"Test case: %s - Message should contain '%s', got: '%s'",
				tt.description, tt.expectedMessagePart, result.Message)
		})
	}
}

func TestPanicValueIsNotReturned(t *testing.T) {
	result := heartbeat.ExecuteHandlerWithTimeout(context.Background(), func() heartbeat.StatusResult {
		panic("dsn=postgres://u:pw@db:5432")
	}, time.Second)
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "panic")
	assert.NotContains(t, result.Message, "pw@db")
}

func TestCheckFuncIsCancelledOnTimeout(t *testing.T) {
	base := runtime.NumGoroutine()
	for range 50 {
		result := heartbeat.RunCheck(context.Background(), func(ctx context.Context) heartbeat.StatusResult {
			<-ctx.Done()
			return heartbeat.StatusResult{Status: heartbeat.StatusOK}
		}, 5*time.Millisecond)
		assert.Equal(t, heartbeat.StatusCritical, result.Status)
		assert.Contains(t, result.Message, "timeout after 5ms")
	}
	waitForGoroutines(t, base)
}

func TestCustomCheckDurationIsRecorded(t *testing.T) {
	result := heartbeat.RunCheck(context.Background(), func(context.Context) heartbeat.StatusResult {
		time.Sleep(20 * time.Millisecond)
		return heartbeat.StatusResult{Status: heartbeat.StatusOK}
	}, time.Second)
	assert.GreaterOrEqual(t, result.RequestDuration, 20.0)
}
