package workit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	. "github.com/tscolari/workit"
)

// executeFutureWithTimeout will fetch the result of a Future with a given timeout.
// If the timeout is reached, it will return true as the second argument.
// Otherwise it will return the result and false.
// If another error happens during the process it will fail the given `*testing.T`.
func executeFutureWithTimeout(t *testing.T, f Future, timeout time.Duration) (interface{}, bool) {
	result, err := futureWithTimeout(f, timeout)
	if err != nil {
		if !errors.Is(err, ErrExternalContextCanceled) {
			require.NoError(t, err, "getting future result failed")
			return nil, false
		}

		return nil, true
	}

	return result, false
}

func futureWithTimeout(f Future, timeout time.Duration) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := f.Result(ctx)
	return result, err
}

func mustFutureWithTimeout(t *testing.T, f Future, timeout time.Duration) interface{} {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := f.Result(ctx)
	require.NoError(t, err)
	return result
}

func consistently(t *testing.T, expectation func() bool, period time.Duration) {
	tickInterval := period / 20
	timeout := time.After(period)

	for {
		select {
		case <-timeout:
			require.True(t, expectation())
			return

		case <-time.Tick(tickInterval):
			require.True(t, expectation())
		}
	}
}
