package coda

type shutdownOptions struct {
	logger Logger
}

func defaultShutdownOptions() *shutdownOptions {
	return &shutdownOptions{
		logger: NewNoopLogger(),
	}
}

type ShutdownOption func(*shutdownOptions)

func buildShutdownOptions(opts ...ShutdownOption) *shutdownOptions {
	options := defaultShutdownOptions()
	for _, fn := range opts {
		fn(options)
	}
	return options
}

// WithShutdownLogger sets the logger for Shutdown. Default is the NoopLogger.
func WithShutdownLogger(logger Logger) ShutdownOption {
	return func(options *shutdownOptions) {
		options.logger = logger
	}
}
