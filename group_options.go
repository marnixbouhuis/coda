package coda

import "time"

type groupOptions struct {
	shutdownTimeout time.Duration
}

func defaultGroupOptions() *groupOptions {
	return &groupOptions{
		shutdownTimeout: -1, // Default is no shutdown timeout
	}
}

type GroupOption func(*groupOptions)

func buildGroupOptions(opts ...GroupOption) *groupOptions {
	options := defaultGroupOptions()
	for _, fn := range opts {
		fn(options)
	}
	return options
}

// WithGroupShutdownTimeout sets the time the shutdown Group will wait for a goroutine to exit when the shutdown signal
// is sent. Any value less than 0 will disable the timeout and will cause the shutdown manager to wait indefinitely.
// Default is no timeout (-1).
// It can be passed to the Shutdown.NewGroup function.
func WithGroupShutdownTimeout(d time.Duration) GroupOption {
	return func(options *groupOptions) {
		options.shutdownTimeout = d
	}
}
