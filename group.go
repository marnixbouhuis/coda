package coda

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// GroupCrashError is an error that is returned when a group crashed.
// This means that a goroutine for this group returned with an error.
type GroupCrashError struct {
	GroupName string
}

var _ error = &GroupCrashError{}

func (e *GroupCrashError) Error() string {
	return fmt.Sprintf("group \"%s\" crashed", e.GroupName)
}

// GroupNotReadyError is an error that is returned when a goroutine did not become ready in time.
// Timeout depends on the timeout settings of the group.
type GroupNotReadyError struct {
	GroupName string
}

var _ error = &GroupNotReadyError{}

func (e *GroupNotReadyError) Error() string {
	return fmt.Sprintf("group \"%s\" did not become ready in time", e.GroupName)
}

// GroupStoppedEarlyError is an error that is returned when a goroutine returned before the context of the group was
// canceled.
type GroupStoppedEarlyError struct {
	GroupName string
}

var _ error = &GroupStoppedEarlyError{}

func (e *GroupStoppedEarlyError) Error() string {
	return fmt.Sprintf("group \"%s\" stopped early, no shutdown signal has been emitted yet but group function still returned", e.GroupName)
}

// Group is a group of goroutines that can be shut down using Shutdown.
// Do not create a Group directly, instead create one using Shutdown.NewGroup.
type Group struct {
	options *groupOptions

	name     string
	shutdown *Shutdown

	isStoppingLock sync.Mutex
	isStopping     bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newGroup(shutdown *Shutdown, name string, opts ...GroupOption) *Group {
	ctx, cancel := context.WithCancel(shutdown.Ctx())
	return &Group{
		options:  buildGroupOptions(opts...),
		name:     name,
		shutdown: shutdown,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Name returns the name of the group.
func (g *Group) Name() string {
	return g.name
}

type GroupFunc func(ctx context.Context, ready func()) error

// Go starts a new goroutine in this group.
// The goroutine is managed by the group and will be shut down when the group
// is shut down.
//
// Options can be provided to customize behavior:
//   - WithReadyTimeout: Maximum time to wait for ready signal
//   - WithCrashOnReadyTimeoutHit: Stop everything if ready timeout is hit
//   - WithCrashOnError: Stop everything if goroutine returns error
//   - WithCrashOnEarlyStop: Stop everything if goroutine exits before shutdown
//   - WithBlock: Block until goroutine signals ready
//
// If the group is already stopping/stopped, the goroutine will not be started.
//
// Example:
//
//	group.Go(func(ctx context.Context, ready func()) error {
//	    // Do initialization
//	    ready()
//
//	    // Wait for shutdown
//	    <-ctx.Done()
//
//	    // Do cleanup
//	    return nil
//	}, coda.WithBlock(true))
func (g *Group) Go(fn GroupFunc, opts ...GroupGoroutineOption) {
	g.isStoppingLock.Lock()
	if g.isStopping {
		// Not starting new goroutine since we are already stopped / in the process of stopping
		g.isStoppingLock.Unlock()
		return
	}

	options := buildGroupGoroutineOptions(opts...)

	readyCh := make(chan struct{})
	var once sync.Once

	ready := func() {
		once.Do(func() {
			close(readyCh)
		})
	}

	g.wg.Add(1)
	go func() {
		defer g.wg.Done()

		if err := fn(g.ctx, ready); err != nil {
			if options.crashOnError {
				g.shutdown.options.logger.Error(fmt.Sprintf("Goroutine in group \"%s\" returned error: %s", g.name, err.Error()))
				g.shutdown.StopWithError(errors.Join(&GroupCrashError{
					GroupName: g.name,
				}, err))
			}
			return
		}

		if g.ctx.Err() == nil && options.crashOnEarlyStop {
			// fn returned, but its context is not canceled yet.
			g.shutdown.options.logger.Error(fmt.Sprintf("Goroutine in group \"%s\" stopped early, crashing...", g.name))
			g.shutdown.StopWithError(&GroupStoppedEarlyError{
				GroupName: g.name,
			})
			return
		}

		g.shutdown.options.logger.Info(fmt.Sprintf("Goroutine in group \"%s\" stopped", g.name))
	}()

	// We can now safely unlock the isStoppingLock since the goroutine is now "added" to the wait group
	g.isStoppingLock.Unlock()

	// Create a channel to signal if we should stop blocking (ready function called or ready timeout hit).
	blockCh := make(chan struct{})

	// Handle ready timeout, do this in a goroutine so we can optionally block on this based on the options provided.
	go func() {
		// Wait for ready function to be called
		if options.readyTimeout < 0 {
			// No ready timeout set, just wait on the readyCh without timeout
			<-readyCh
			close(blockCh)
			return
		}

		// Timeout is set, wait with timeout
		timer := time.NewTimer(options.readyTimeout)
		select {
		case <-readyCh:
			// Make sure we don't leak any resources
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			g.shutdown.options.logger.Error(fmt.Sprintf("Ready timeout hit for goroutine in group \"%s\"", g.name))

			// Timed out waiting for readyCh to be closed
			if options.crashOnReadyTimeoutHit {
				g.shutdown.StopWithError(&GroupNotReadyError{
					GroupName: g.name,
				})
			}
		}

		close(blockCh)
	}()

	if options.block {
		<-blockCh
	}
}

// stopAndWait will send the signal to all goroutines running in this group to shut down.
// It will wait until all routines have stopped (until a timeout is hit if a timeout is set).
func (g *Group) stopAndWait() (timedOut bool) {
	g.isStoppingLock.Lock()
	g.isStopping = true
	g.isStoppingLock.Unlock()

	g.cancel()

	if g.options.shutdownTimeout < 0 {
		// No need to handle timeouts, etc...
		// Just wait for all routines to be done
		g.wg.Wait()
		return false
	}

	// Timeout is set, wait using a timeout
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.wg.Wait()
	}()

	timer := time.NewTimer(g.options.shutdownTimeout)
	select {
	case <-done:
		// Make sure we don't leak any resources
		if !timer.Stop() {
			<-timer.C
		}
		return false
	case <-timer.C:
		return true
	}
}
