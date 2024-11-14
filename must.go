package coda

// Must is a helper that wraps a function call returning (T, error) and panics if the error is non-nil.
// It's useful for operations that should never fail during normal execution and simplifies initialization code.
//
// Example:
//
//	// Instead of:
//	group, err := shutdown.NewGroup("api", nil)
//	if err != nil {
//	    panic(err)
//	}
//
//	// You can write:
//	group := coda.Must(shutdown.NewGroup("api", nil))
//
// Note: Only use Must in initialization code where a panic is acceptable.
// For runtime operations, handle errors explicitly instead.
func Must[T any](res T, err error) T {
	if err != nil {
		panic(err)
	}
	return res
}
