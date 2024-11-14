package coda

import "log"

// Logger defines the interface for custom logging implementations for coda.
type Logger interface {
	// Info logs an informational message
	Info(str string)
	// Error logs an error message
	Error(str string)
}

// StdLogger is a logger that adapts Logger to a *log.Logger from the standard library.
type StdLogger struct {
	l *log.Logger
}

var _ Logger = &StdLogger{}

// NewStdLogger creates a new Logger that wraps the standard library's log.Logger.
// It provides a simple way to integrate with Go's built-in logging.
//
// Example:
//
//	logger := coda.NewStdLogger(log.Default())
//	sd := coda.NewShutdown(coda.WithShutdownLogger(logger))
func NewStdLogger(logger *log.Logger) Logger {
	return &StdLogger{
		l: logger,
	}
}

func (l *StdLogger) Info(str string) {
	l.l.Println("info: ", str)
}

func (l *StdLogger) Error(str string) {
	l.l.Println("error: ", str)
}

// NoopLogger is a logger that discards all output.
type NoopLogger struct{}

var _ Logger = &NoopLogger{}

// NewNoopLogger creates a new NoopLogger. This logger discards all output.
func NewNoopLogger() Logger {
	return &NoopLogger{}
}

func (*NoopLogger) Info(_ string)  {}
func (*NoopLogger) Error(_ string) {}
