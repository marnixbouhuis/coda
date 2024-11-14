package coda_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/marnixbouhuis/coda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroup(t *testing.T) {
	t.Parallel()

	t.Run("it should stop all goroutines and wait for them to shutdown when the shutdown manager is stopped", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		var wg sync.WaitGroup
		f := func(ctx context.Context, ready func()) error {
			ready()
			<-ctx.Done()
			wg.Done()
			return nil
		}

		wg.Add(3)
		g.Go(f)
		g.Go(f)
		g.Go(f)

		s.Stop()

		// Make sure all goroutines stop
		stopped := make(chan struct{})
		go func() {
			wg.Wait()
			close(stopped)
		}()

		select {
		case <-stopped:
			// Done! all goroutines stopped
		case <-time.After(time.Second):
			assert.Fail(t, "timed out while waiting for goroutines to stop, did all routines get the shutdown signal?")
		}
	})

	t.Run("it should not block when starting go routines if disabled", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		f := func(ctx context.Context, ready func()) error {
			<-time.After(2 * time.Second) // Add a delay before calling ready to make sure we are not blocking
			ready()
			<-ctx.Done()
			return nil
		}

		now := time.Now()
		g.Go(f, coda.WithBlock(false))
		g.Go(f, coda.WithBlock(false))
		g.Go(f, coda.WithBlock(false))
		// Make sure we started in less than 1 second.
		// The go function waits for 2 seconds on startup, we should not wait for this when blocking is disabled.
		assert.Less(t, time.Since(now), time.Second, "Should start without blocking")
	})

	t.Run("it should block when starting go routines if enabled", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		f := func(ctx context.Context, ready func()) error {
			<-time.After(2 * time.Second) // Add a delay before calling ready to make sure we are not blocking
			ready()
			<-ctx.Done()
			return nil
		}

		now := time.Now()
		g.Go(f, coda.WithBlock(true))
		g.Go(f, coda.WithBlock(true))
		g.Go(f, coda.WithBlock(true))
		// Make sure we started in less than 1 second.
		// The go function waits for 2 seconds on startup, we should wait for this when blocking is enabled.
		assert.Greater(t, time.Since(now), 2*3*time.Second, "Should start with blocking")
	})

	t.Run("it should max block until the ready timeout has been hit", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		f := func(ctx context.Context, ready func()) error {
			<-time.After(30 * time.Second) // Extremely high delay to make sure we don't fully wait for this
			ready()
			<-ctx.Done()
			return nil
		}

		now := time.Now()
		g.Go(f, coda.WithBlock(true), coda.WithReadyTimeout(time.Second), coda.WithCrashOnReadyTimeoutHit(false))
		g.Go(f, coda.WithBlock(true), coda.WithReadyTimeout(time.Second), coda.WithCrashOnReadyTimeoutHit(false))
		g.Go(f, coda.WithBlock(true), coda.WithReadyTimeout(time.Second), coda.WithCrashOnReadyTimeoutHit(false))
		// Make sure we started in around 3 seconds.
		// The go function waits for 30 seconds on startup, but the ready timeout is 1 second.
		assert.WithinDuration(t, now.Add(3*time.Second), time.Now(), 500*time.Millisecond)
	})

	t.Run("it should not crash the shutdown handler on ready timeout hit if this is disabled", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		f := func(ctx context.Context, ready func()) error {
			<-time.After(5 * time.Second) // Extremely high delay to make sure we don't fully wait for this
			ready()
			<-ctx.Done()
			return nil
		}
		g.Go(f,
			coda.WithBlock(true),
			coda.WithReadyTimeout(time.Second),
			coda.WithCrashOnReadyTimeoutHit(false),
		)

		s.Stop()

		assert.NoError(t, s.Wait())
	})

	t.Run("it should crash the shutdown handler on ready timeout hit if this is enabled", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		f := func(ctx context.Context, ready func()) error {
			<-time.After(5 * time.Second) // Extremely high delay to make sure we don't fully wait for this
			ready()
			<-ctx.Done()
			return nil
		}
		g.Go(f,
			coda.WithBlock(true),
			coda.WithReadyTimeout(time.Second),
			coda.WithCrashOnReadyTimeoutHit(true),
		)

		// No need to call s.Stop() since a crash should auto stop the handler

		var actualErr *coda.GroupNotReadyError
		require.ErrorAs(t, s.Wait(), &actualErr)
		assert.Equal(t, "group", actualErr.GroupName)
	})

	t.Run("it should crash the shutdown handler on ready timeout hit if this is enabled even when blocking is disabled", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		f := func(ctx context.Context, ready func()) error {
			<-time.After(5 * time.Second) // Extremely high delay to make sure we don't fully wait for this
			ready()
			<-ctx.Done()
			return nil
		}
		g.Go(f,
			coda.WithBlock(false),
			coda.WithReadyTimeout(time.Second),
			coda.WithCrashOnReadyTimeoutHit(true),
		)

		// No need to call s.Stop() since a crash should auto stop the handler.
		// We still don't block so g.Go should continue immediately.

		var actualErr *coda.GroupNotReadyError
		require.ErrorAs(t, s.Wait(), &actualErr)
		assert.Equal(t, "group", actualErr.GroupName)
	})

	t.Run("it should not crash the shutdown handler if the goroutine errors and crashing on this is disabled", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		testErr := errors.New("some error")

		f := func(_ context.Context, ready func()) error {
			ready()
			return testErr
		}
		g.Go(f, coda.WithCrashOnError(false), coda.WithBlock(true))

		// Wait a bit to make sure that the shutdown cloud have been processed in the background
		<-time.After(time.Second)

		s.Stop()

		assert.NoError(t, s.Wait())
	})

	t.Run("it should crash the shutdown handler if the goroutine errors and this is enabled", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		testErr := errors.New("some error")

		f := func(_ context.Context, _ func()) error {
			return testErr
		}
		g.Go(f, coda.WithCrashOnError(true))

		// No need to call s.Stop() since a crash should auto stop the handler.
		// We still don't block so g.Go should continue immediately.

		err = s.Wait()

		var actualErr *coda.GroupCrashError
		require.ErrorAs(t, s.Wait(), &actualErr)
		assert.Equal(t, "group", actualErr.GroupName)

		// Make sure the original error is still in the chain
		require.ErrorIs(t, err, testErr)
	})

	t.Run("starting a new goroutine on a shutdown handler that is stopped should not call the goroutine", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		s.Stop()

		require.NoError(t, s.Wait())

		var called bool
		g.Go(func(_ context.Context, _ func()) error {
			called = true
			return nil
		})

		<-time.After(time.Second)
		assert.False(t, called)
	})

	t.Run("the shutdown handler should crash if a goroutine stops early and this is configured to do so", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		f := func(_ context.Context, ready func()) error {
			ready()
			<-time.After(time.Second) // Stop after 1 second, even though the context is not canceled
			return nil
		}
		g.Go(f, coda.WithCrashOnEarlyStop(true))

		var actualErr *coda.GroupStoppedEarlyError
		require.ErrorAs(t, s.Wait(), &actualErr)
		assert.Equal(t, "group", actualErr.GroupName)
	})

	t.Run("the shutdown handler should not crash if a goroutine stops early and crashing on this is disabled", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown()
		g, err := s.NewGroup("group", nil)
		require.NoError(t, err)

		f := func(_ context.Context, ready func()) error {
			ready()
			<-time.After(time.Second) // Stop after 1 second, even though the context is not canceled
			return nil
		}
		g.Go(f, coda.WithCrashOnEarlyStop(false))

		// Wait a bit in order for the goroutine to exit already
		<-time.After(3 * time.Second)
		s.Stop()
		assert.NoError(t, s.Wait())
	})
}
