package coda_test

import (
	"errors"
	"testing"

	"github.com/marnixbouhuis/coda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMust(t *testing.T) {
	t.Parallel()

	t.Run("should panic on error", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("look at me, I'm an error")
		fn := func() (string, error) {
			return "", expectedErr
		}

		require.PanicsWithError(t, expectedErr.Error(), func() {
			coda.Must(fn())
		})
	})

	t.Run("should not panic on success", func(t *testing.T) {
		t.Parallel()

		fn := func() (string, error) {
			return "success", nil
		}

		require.NotPanics(t, func() {
			res := coda.Must(fn())
			assert.Equal(t, "success", res)
		})
	})
}
