package coda_test

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/marnixbouhuis/coda"
)

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
		mux := http.NewServeMux()
		srv := &http.Server{
			Addr:         ":8080",
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}

		mux.HandleFunc("/demo", func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				_ = r.Body.Close()
			}()
			w.WriteHeader(http.StatusOK)
		})

		go func() {
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				log.Printf("HTTP server error: %v", err)
			}
		}()

		ready()
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
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
