package coda

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrAlreadyStopped    = errors.New("already stopped")
	ErrInvalidDependency = errors.New("invalid dependency")
)

// Shutdown manages the graceful shutdown of goroutine groups in a coordinated way.
// It ensures groups are shut down in the correct order based on their dependencies.
// Create a new instance using NewShutdown().
type Shutdown struct {
	options *shutdownOptions

	rootCtx    context.Context
	cancelRoot func()

	shutdownErrorsLock sync.Mutex
	shutdownErrors     []error

	stopGroupsOnce sync.Once

	addGroupsLock sync.Mutex
	isStopping    bool
	groups        map[*Group][]*Group
}

// NewShutdown creates a new shutdown manager instance.
// The shutdown manager coordinates the graceful shutdown of goroutine groups
// and ensures they stop in the correct dependency order.
//
// Options can be provided to customize behavior:
//   - WithShutdownLogger: Sets a custom logger for shutdown events
//
// Example:
//
//	sd := coda.NewShutdown(
//	    coda.WithShutdownLogger(coda.NewStdLogger(log.Default())),
//	)
//
//	// Create groups with dependencies
//	dbGroup := coda.Must(sd.NewGroup("database", nil))
//	apiGroup := coda.Must(sd.NewGroup("api", []*coda.Group{dbGroup}))
//
//	// ...start goroutines for groups
//
//	go func() {
//		ch := make(chan os.Signal, 1)
//		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
//		<-ch
//		sd.Stop()
//	}()
//
//	// Wait for shutdown to complete
//	if err := sd.Wait(); err != nil {
//	    log.Fatal(err)
//	}
func NewShutdown(opts ...ShutdownOption) *Shutdown {
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	return &Shutdown{
		options:        buildShutdownOptions(opts...),
		rootCtx:        rootCtx,
		cancelRoot:     cancelRoot,
		shutdownErrors: make([]error, 0),
		groups:         make(map[*Group][]*Group),
	}
}

// NewGroup creates a new group with the given name and dependencies.
// Groups are collections of goroutines that should be shut down together.
// Dependencies define the shutdown order - a group will not be shut down until
// all groups that depend on it are shut down first.
//
// The name must be unique within this shutdown manager.
// Dependencies must belong to the same shutdown manager instance.
//
// Options can be provided to customize behavior:
//   - WithGroupShutdownTimeout: Maximum time to wait for group shutdown
//
// Returns an error if:
//   - The shutdown manager is already stopping/stopped (ErrAlreadyStopped)
//   - A dependency belongs to a different shutdown manager (ErrInvalidDependency)
//   - The same dependency is listed multiple times (ErrInvalidDependency)
func (s *Shutdown) NewGroup(name string, dependencies []*Group, opts ...GroupOption) (*Group, error) {
	s.addGroupsLock.Lock()
	defer s.addGroupsLock.Unlock()

	if s.isStopping {
		// Can't add group since we are already stopping
		return nil, ErrAlreadyStopped
	}

	// Circular dependencies will cause deadlocks, two groups waiting for each other on shutdown.
	// Since the only way to create a group is using NewGroup this should not be possible.

	// Make sure all dependencies are part of this shutdown handler and make sure there are no duplicates.
	knownDeps := make(map[*Group]struct{})
	for _, dep := range dependencies {
		if _, ok := s.groups[dep]; !ok {
			return nil, fmt.Errorf("%w: dependency \"%s\" is not part of this shutdown instance", ErrInvalidDependency, dep.Name())
		}

		if _, ok := knownDeps[dep]; ok {
			// Dependency list contains a duplicate, error
			return nil, fmt.Errorf("%w: duplicate group \"%s\" in dependency list", ErrInvalidDependency, dep.Name())
		}
		knownDeps[dep] = struct{}{}
	}

	g := newGroup(s, name, opts...)
	s.groups[g] = dependencies
	return g, nil
}

// Ctx returns the root context for this shutdown manager.
// The context is canceled when shutdown completes.
func (s *Shutdown) Ctx() context.Context {
	return s.rootCtx
}

// Stop initiates a graceful shutdown of all groups.
// It signals all groups to stop and waits for them to complete in dependency order.
// Multiple calls to Stop are supported and will be handled gracefully.
func (s *Shutdown) Stop() {
	s.StopWithError(nil)
}

// StopWithError initiates a graceful shutdown with an error.
// The error will be returned by Wait() after shutdown completes.
// Multiple calls are supported and errors will be joined together.
func (s *Shutdown) StopWithError(err error) {
	if err != nil {
		s.options.logger.Error("Stopping with error: " + err.Error())

		s.shutdownErrorsLock.Lock()
		s.shutdownErrors = append(s.shutdownErrors, err)
		s.shutdownErrorsLock.Unlock()
	}

	go s.stopGroupsOnce.Do(func() {
		s.stopGroups()
	})
}

func (s *Shutdown) stopGroups() {
	s.options.logger.Info("Stopping groups...")

	s.addGroupsLock.Lock()
	s.isStopping = true // Disable adding more groups, by indicating that we are stopping.
	s.addGroupsLock.Unlock()

	// Determine shutdown order, find nodes with no edges pointing to it.
	// These are the nodes (groups) that no other groups depend on.
	var inDegreeLock sync.Mutex
	inDegree := make(map[*Group]int)
	for group, dependencies := range s.groups {
		if _, hasGroup := inDegree[group]; !hasGroup {
			inDegree[group] = 0
		}

		for _, dependency := range dependencies {
			inDegree[dependency]++
		}
	}

	// Get a list of groups that we can start shutdown with, these are groups that have no incoming dependency edges.
	initialGroups := make([]*Group, 0, len(inDegree))
	for group, dependentGroupsCount := range inDegree {
		if dependentGroupsCount == 0 {
			// This group has no other groups that depend on it
			initialGroups = append(initialGroups, group)
		}
	}

	var shutdownWg sync.WaitGroup

	var stopGroup func(group *Group)
	stopGroup = func(group *Group) {
		shutdownWg.Add(1)
		go func() {
			defer shutdownWg.Done()

			// Shut down the group
			s.options.logger.Info(fmt.Sprintf("Shutting down group \"%s\"", group.Name()))
			if timedOut := group.stopAndWait(); timedOut {
				s.options.logger.Error(fmt.Sprintf("Timed-out while waiting for group \"%s\" to stop, continuing without waiting...", group.Name()))
			}

			s.options.logger.Info(fmt.Sprintf("Shutting down dependencies of group \"%s\"...", group.Name()))

			// Group is now stopped. Check if this group had any groups it depended on.
			for _, dependency := range s.groups[group] {
				inDegreeLock.Lock()

				s.options.logger.Info(fmt.Sprintf("Removing group \"%s\" from dependency count of group \"%s\"", group.Name(), dependency.Name()))

				// decrement inDegree for dependency, we stopped a group that pointed to this other group.
				score := inDegree[dependency] - 1
				inDegree[dependency] = score

				inDegreeLock.Unlock()

				// Check if the inDegree score of this group is now 0, this means that we can safely shut down the
				// dependency.
				if score == 0 {
					stopGroup(dependency)
				}
			}
		}()
	}

	for _, group := range initialGroups {
		stopGroup(group)
	}

	// Wait for all groups to be stopped
	shutdownWg.Wait()

	// Sanity check, make sure that all groups have an inDegree score of 0 and that all groups have actually been stopped.
	for group, edgeCount := range inDegree {
		if edgeCount != 0 {
			s.options.logger.Error(fmt.Sprintf("Shutdown error, group \"%s\" still has dependencies (%d) after whole graph has been shut down", group.Name(), edgeCount))
		}
	}

	// All groups have been shut down, cancel the root ctx
	s.options.logger.Info("All groups stopped, canceling root context...")
	s.cancelRoot()
}

// Wait blocks until all groups have been shut down. It returns any shutdown errors that might have occurred.
// If Shutdown is already fully stopped it will return immediately (with any shutdown errors if present).
// Multiple calls to Wait are supported.
func (s *Shutdown) Wait() error {
	<-s.rootCtx.Done()

	s.shutdownErrorsLock.Lock()
	defer s.shutdownErrorsLock.Unlock()

	return errors.Join(s.shutdownErrors...)
}
