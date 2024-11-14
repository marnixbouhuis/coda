package coda

import "time"

type groupGoroutineOptions struct {
	readyTimeout           time.Duration
	crashOnReadyTimeoutHit bool
	crashOnError           bool
	crashOnEarlyStop       bool
	block                  bool
}

func defaultGroupGoroutineOptions() *groupGoroutineOptions {
	return &groupGoroutineOptions{
		readyTimeout:           -1, // No timeout
		crashOnReadyTimeoutHit: true,
		crashOnError:           true,
		crashOnEarlyStop:       true,
		block:                  false,
	}
}

type GroupGoroutineOption func(*groupGoroutineOptions)

func buildGroupGoroutineOptions(opts ...GroupGoroutineOption) *groupGoroutineOptions {
	options := defaultGroupGoroutineOptions()
	for _, fn := range opts {
		fn(options)
	}
	return options
}

// WithReadyTimeout sets the timeout Group.Go will max block for while waiting for the ready() signal.
// Any value less than 0 will disable the timeout and will cause Go to wait without a limit.
// Default is no timeout.
// It can be passed to Group.Go.
func WithReadyTimeout(d time.Duration) GroupGoroutineOption {
	return func(options *groupGoroutineOptions) {
		options.readyTimeout = d
	}
}

// WithCrashOnReadyTimeoutHit sets if Shutdown should fully stop / abort startup if the ready timeout is hit.
// Set to "false" to ignore timeout errors and continue normal execution, set to "true" to auto shutdown.
// Default is "true".
// It can be passed to Group.Go.
func WithCrashOnReadyTimeoutHit(crash bool) GroupGoroutineOption {
	return func(options *groupGoroutineOptions) {
		options.crashOnReadyTimeoutHit = crash
	}
}

// WithCrashOnError sets if Shutdown should fully stop with an error if the GroupFunc returns
// an error.
// Default is "true".
// It can be passed to Group.Go.
func WithCrashOnError(crash bool) GroupGoroutineOption {
	return func(options *groupGoroutineOptions) {
		options.crashOnError = crash
	}
}

// WithCrashOnEarlyStop sets if Shutdown should fully stop with an error if the GroupFunc returns
// early. An early return is when the function returns without an error and the context of the group is not canceled.
// Default is "true".
// It can be passed to Group.Go.
func WithCrashOnEarlyStop(crash bool) GroupGoroutineOption {
	return func(options *groupGoroutineOptions) {
		options.crashOnEarlyStop = crash
	}
}

// WithBlock sets whether Group.Go should block until the goroutine signals it's ready.
// When true, Go will not return until either:
//   - The ready() function is called
//   - The ready timeout is hit (if configured)
//   - The goroutine returns an error
//
// Default is "false" (non-blocking).
// It can be passed to Group.Go.
func WithBlock(block bool) GroupGoroutineOption {
	return func(options *groupGoroutineOptions) {
		options.block = block
	}
}
