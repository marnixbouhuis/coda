# Coda - Graceful Shutdown Manager for Go
[![Go Reference](https://pkg.go.dev/badge/github.com/marnixbouhuis/coda.svg)](https://pkg.go.dev/github.com/marnixbouhuis/coda)
[![CI/CD Pipeline](https://github.com/MarnixBouhuis/coda/actions/workflows/cicd.yaml/badge.svg)](https://github.com/MarnixBouhuis/coda/actions/workflows/cicd.yaml)


Coda is a Go library that helps manage graceful shutdowns of concurrent applications. It provides a structured way to organize goroutines into groups with dependencies and ensures they shut down in the correct order.

## Features

- Group-based goroutine management
- Dependency-based shutdown ordering
- Configurable timeouts and behavior
- Flexible logging options
- Error handling and propagation

## Installation

```bash
go get github.com/marnixbouhuis/coda
```

## Quick Start

```go
shutdown := coda.NewShutdown()

// Create groups
dbGroup := coda.Must(shutdown.NewGroup("database", nil))
workerGroup := coda.Must(shutdown.NewGroup("worker", []*coda.Group{dbGroup}))

// Add goroutines to groups
dbGroup.Go(func(ctx context.Context, ready func()) error {
	// Do startup work here
	ready()
	
	// Wait for shutdown signal
	<-ctx.Done()
	
	// Do tear down logic here
	
	return nil
})

// Wait for shutdown
err := shutdown.Wait()
```

## Core Concepts

### Shutdown Manager
The shutdown manager is the main coordinator that manages groups and their shutdown sequence. Create one using `NewShutdown()`.

Options:
- `WithShutdownLogger(logger Logger)` - Set a custom logger (default: NoopLogger)

### Groups
Groups are collections of goroutines that should be shut down together. Groups can depend on other groups, creating a shutdown hierarchy.

Create groups using `shutdown.NewGroup(name string, dependencies []*Group, opts ...GroupOption)`.

Group options:
- `WithGroupShutdownTimeout(duration)` - Maximum time to wait for group shutdown (default: no timeout)

### Goroutines
Add goroutines to groups using the `Go()` method. Each goroutine receives a context and a ready function.

```go
group.Go(func(ctx context.Context, ready func()) error {
	// Do startup here

	// Signal readiness
	ready()

	// Wait for shutdown
	<-ctx.Done()

	// Do shutdown here

	return nil
}, opts...)
```

Goroutine options:
- `WithReadyTimeout(duration)` - Maximum time to wait for ready signal (default: no timeout)
- `WithCrashOnReadyTimeoutHit(bool)` - Stop everything if ready timeout is hit (default: true)
- `WithCrashOnError(bool)` - Stop everything if goroutine returns error (default: true)
- `WithCrashOnEarlyStop(bool)` - Stop everything if goroutine exits before shutdown (default: true)
- `WithBlock(bool)` - Block until goroutine signals ready (default: false)

## Examples

### Multiple Groups with Dependencies

```go
func Example() {
	sd := coda.NewShutdown(
		coda.WithShutdownLogger(coda.NewStdLogger(log.Default())),
	)

	// Create groups with dependencies
	dbGroup := coda.Must(sd.NewGroup("database", nil,
		coda.WithGroupShutdownTimeout(5*time.Second),
	))

	workerGroup := coda.Must(sd.NewGroup("workers", []*coda.Group{dbGroup},
		coda.WithGroupShutdownTimeout(10*time.Second),
	))

	// Database connection
	dbGroup.Go(func(ctx context.Context, ready func()) error {
		db, err := sql.Open("postgres", "connection-string")
		if err != nil {
			return err
		}
		defer db.Close()

		ready()
		<-ctx.Done()
		return nil
	}, coda.WithBlock(true))

	// Start multiple workers
	for workerID := range 3 {
		workerGroup.Go(func(ctx context.Context, ready func()) error {
			log.Printf("Worker %d starting", workerID)

			ready()

			for {
				select {
				case <-ctx.Done():
					log.Printf("Worker %d shutting down", workerID)
					return nil
				case <-time.After(time.Second):
					// Do some work
					log.Printf("Worker %d processing", workerID)
				}
			}
		}, coda.WithBlock(true))
	}

	serverGroup := coda.Must(sd.NewGroup("server", []*coda.Group{workerGroup, dbGroup}))
	serverGroup.Go(func(ctx context.Context, ready func()) error {
		ready()

		mux := http.NewServeMux()
		srv := &http.Server{
			Addr:         ":8080",
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}

		mux.HandleFunc("/demo", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		go func() {
			<-ctx.Done()

			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second*30)
			defer cancel()

			if err := srv.Shutdown(shutdownCtx); err != nil {
				log.Printf("Failed to stop HTTP server gracefully: %v", err)
				sd.StopWithError(err)
			}
		}()

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		<-ch
		sd.Stop()
	}()

	if err := sd.Wait(); err != nil {
		log.Printf("Shutdown error: %v", err)
		os.Exit(1)
	}
}
```

## Error Handling

Errors can occur in several ways:
- Goroutine returns an error
- Ready timeout is hit
- Goroutine stops early
- Shutdown timeout is hit

Configure how these errors are handled using the appropriate options. Errors can either crash the entire shutdown process or be logged and ignored.

## Logging

Coda supports custom logging through the `Logger` interface:

```go
type Logger interface {
	Info(str string)
	Error(str string)
}
```

Built-in loggers:
- `NewNoopLogger()` - Discards all logs (default)
- `NewStdLogger(logger *log.Logger)` - Adapts standard library logger

## License

This project is licensed under the MIT License - see the [LICENSE.md](LICENSE.md) file for details.