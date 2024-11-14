package coda_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/marnixbouhuis/coda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLogger is a coda logger that automatically fails the test if an error is logged.
type testLogger struct {
	t              *testing.T
	expectedErrors []string
}

var _ coda.Logger = &testLogger{}

func newTestLogger(t *testing.T, expectedErrors []string) *testLogger {
	t.Helper()
	return &testLogger{
		t:              t,
		expectedErrors: expectedErrors,
	}
}

func (l *testLogger) Info(str string) {
	l.t.Helper()
	l.t.Log("INFO:", str)
}

func (l *testLogger) Error(str string) {
	l.t.Helper()
	l.t.Log("ERROR:", str)
	if !slices.Contains(l.expectedErrors, str) {
		l.t.Log("Error is unexpected")
		l.t.Fail()
	}
}

func TestShutdown(t *testing.T) {
	t.Parallel()

	t.Run("it should block on Wait until Stop is called when no groups are present", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))

		done := make(chan struct{})
		go func() {
			err := s.Wait()
			assert.NoError(t, err)
			close(done)
		}()

		select {
		case <-done:
			assert.Fail(t, "Wait should block until Stop is called")
		case <-time.After(time.Second):
			// Expected behavior - Wait is blocking
		}

		s.Stop()

		select {
		case <-done:
			// Expected behavior - Wait returned after Stop
		case <-time.After(time.Second):
			assert.Fail(t, "Wait should return after Stop is called")
		}
	})

	t.Run("it should properly shut down a single group", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))
		g, err := s.NewGroup("test", nil)
		require.NoError(t, err)

		var stopped bool
		g.Go(func(ctx context.Context, ready func()) error {
			ready()
			<-ctx.Done()
			stopped = true
			return nil
		})

		s.Stop()
		require.NoError(t, s.Wait())
		assert.True(t, stopped, "group should have been stopped")
	})

	t.Run("it should properly shut down multiple groups with no dependencies", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))
		g1, err := s.NewGroup("group1", nil)
		require.NoError(t, err)
		g2, err := s.NewGroup("group2", nil)
		require.NoError(t, err)

		var stopped1, stopped2 bool
		g1.Go(func(ctx context.Context, ready func()) error {
			ready()
			<-ctx.Done()
			stopped1 = true
			return nil
		})
		g2.Go(func(ctx context.Context, ready func()) error {
			ready()
			<-ctx.Done()
			stopped2 = true
			return nil
		})

		s.Stop()
		require.NoError(t, s.Wait())
		assert.True(t, stopped1, "group1 should have been stopped")
		assert.True(t, stopped2, "group2 should have been stopped")
	})

	t.Run("it should properly shut down groups in dependency order", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))
		g1, err := s.NewGroup("group1", nil)
		require.NoError(t, err)
		g2, err := s.NewGroup("group2", []*coda.Group{g1})
		require.NoError(t, err)
		g3, err := s.NewGroup("group3", []*coda.Group{g2})
		require.NoError(t, err)

		var order []string
		g1.Go(func(ctx context.Context, ready func()) error {
			ready()
			<-ctx.Done()
			order = append(order, "group1")
			return nil
		})
		g2.Go(func(ctx context.Context, ready func()) error {
			ready()
			<-ctx.Done()
			order = append(order, "group2")
			return nil
		})
		g3.Go(func(ctx context.Context, ready func()) error {
			ready()
			<-ctx.Done()
			order = append(order, "group3")
			return nil
		})

		s.Stop()
		require.NoError(t, s.Wait())
		assert.Equal(t, []string{"group3", "group2", "group1"}, order)
	})

	t.Run("it should properly handle complex dependency chains during shutdown", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))

		// Create a more complex dependency graph:
		// g1 <- g2 <- g4
		//    <- g3 <-/
		g1, err := s.NewGroup("g1", nil)
		require.NoError(t, err)
		g2, err := s.NewGroup("g2", []*coda.Group{g1})
		require.NoError(t, err)
		g3, err := s.NewGroup("g3", []*coda.Group{g1})
		require.NoError(t, err)
		g4, err := s.NewGroup("g4", []*coda.Group{g2, g3})
		require.NoError(t, err)

		var order []string
		var orderLock sync.Mutex
		groupFunc := func(name string) coda.GroupFunc {
			return func(ctx context.Context, ready func()) error {
				ready()
				<-ctx.Done()

				orderLock.Lock()
				order = append(order, name)
				orderLock.Unlock()

				return nil
			}
		}

		g1.Go(groupFunc("g1"))
		g2.Go(groupFunc("g2"))
		g3.Go(groupFunc("g3"))
		g4.Go(groupFunc("g4"))

		s.Stop()
		require.NoError(t, s.Wait())

		// g4 should stop first, then g2 and g3 (order between them doesn't matter), then g1
		assert.Equal(t, "g4", order[0], "g4 should stop first")
		assert.Contains(t, order[1:3], "g2", "g2 should stop after g4")
		assert.Contains(t, order[1:3], "g3", "g3 should stop after g4")
		assert.Equal(t, "g1", order[3], "g1 should stop last")
	})

	t.Run("it should support multiple calls to Wait", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{
			"Stopping with error: test error",
		})))

		testErr := errors.New("test error")

		// Start multiple goroutines waiting on shutdown
		var wg sync.WaitGroup
		wg.Add(3)

		for range 3 {
			go func() {
				defer wg.Done()
				err := s.Wait()
				assert.ErrorIs(t, err, testErr)
			}()
		}

		s.StopWithError(testErr)

		// Wait for all goroutines to complete
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Expected - all Wait calls returned
		case <-time.After(time.Second):
			assert.Fail(t, "Wait calls did not all return in time")
		}
	})

	t.Run("Wait should return immediately after shutdown has been stopped", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))
		s.Stop()

		done := make(chan struct{})
		go func() {
			assert.NoError(t, s.Wait())
			close(done)
		}()

		select {
		case <-done:
			// Expected behavior - Wait returned immediately
		case <-time.After(time.Second):
			assert.Fail(t, "Wait should return immediately after shutdown is stopped")
		}
	})

	t.Run("multiple calls to Stop should be supported", func(t *testing.T) {
		t.Parallel()

		t.Run("with no errors", func(t *testing.T) {
			t.Parallel()

			s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))

			// Call Stop multiple times
			for range 3 {
				s.Stop()
			}

			require.NoError(t, s.Wait())
		})

		t.Run("with multiple errors", func(t *testing.T) {
			t.Parallel()

			s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{
				"Stopping with error: error 1",
				"Stopping with error: error 2",
				"Stopping with error: error 3",
			})))

			err1 := errors.New("error 1")
			err2 := errors.New("error 2")
			err3 := errors.New("error 3")

			s.StopWithError(err1)
			s.StopWithError(err2)
			s.StopWithError(err3)

			err := s.Wait()
			require.ErrorIs(t, err, err1)
			require.ErrorIs(t, err, err2)
			require.ErrorIs(t, err, err3)
		})

		t.Run("with mixed Stop and StopWithError calls", func(t *testing.T) {
			t.Parallel()

			s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{
				"Stopping with error: error 1",
				"Stopping with error: error 2",
			})))

			err1 := errors.New("error 1")
			err2 := errors.New("error 2")

			s.Stop()              // No error
			s.StopWithError(err1) // With error
			s.Stop()              // No error
			s.StopWithError(err2) // With error
			s.Stop()              // No error

			err := s.Wait()
			require.ErrorIs(t, err, err1)
			require.ErrorIs(t, err, err2)
		})
	})

	t.Run("it should error when adding a dependency from another shutdown manager", func(t *testing.T) {
		t.Parallel()

		s1 := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))
		s2 := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))

		g1, err := s1.NewGroup("group1", nil)
		require.NoError(t, err)

		_, err = s2.NewGroup("group2", []*coda.Group{g1})
		require.ErrorIs(t, err, coda.ErrInvalidDependency)
		assert.Contains(t, err.Error(), "dependency \"group1\" is not part of this shutdown instance")
	})

	t.Run("it should error when adding duplicate dependencies", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))
		g1, err := s.NewGroup("group1", nil)
		require.NoError(t, err)

		_, err = s.NewGroup("group2", []*coda.Group{g1, g1})
		require.ErrorIs(t, err, coda.ErrInvalidDependency)
		require.Contains(t, err.Error(), "duplicate group \"group1\" in dependency list")
	})

	t.Run("it should error when adding a new group to a stopped shutdown handler", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{})))
		s.Stop()
		require.NoError(t, s.Wait())

		_, err := s.NewGroup("group1", nil)
		require.ErrorIs(t, err, coda.ErrAlreadyStopped)
	})

	t.Run("it should respect group shutdown timeout when shutting down", func(t *testing.T) {
		t.Parallel()

		s := coda.NewShutdown(coda.WithShutdownLogger(newTestLogger(t, []string{
			"Timed-out while waiting for group \"test\" to stop, continuing without waiting...",
		})))

		g, err := s.NewGroup("test", nil, coda.WithGroupShutdownTimeout(100*time.Millisecond))
		require.NoError(t, err)

		g.Go(func(ctx context.Context, ready func()) error {
			ready()
			<-ctx.Done()
			// Sleep longer than the shutdown timeout
			time.Sleep(10 * time.Second)
			return nil
		})

		start := time.Now()
		s.Stop()
		require.NoError(t, s.Wait())
		duration := time.Since(start)

		// Verify shutdown completed within expected timeout (with some extra margin)
		assert.Less(t, duration, 200*time.Millisecond, "shutdown took longer than timeout")
	})
}
