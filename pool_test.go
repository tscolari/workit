package workit_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	. "github.com/tscolari/workit"
)

func Test_PoolLifeCycle(t *testing.T) {

	t.Run("Using Stop", func(t *testing.T) {
		pool := NewPool(2, 10)
		done := atomic.Bool{}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			pool.Work(ctx)
			done.Swap(true)
		}()

		pool.Stop(context.Background())

		require.Eventually(t, func() bool {
			return done.Load()
		}, time.Second, 10*time.Millisecond)

		t.Run("wait for pending tasks", func(t *testing.T) {
			pool := NewPool(2, 10)
			done := atomic.Bool{}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				pool.Work(ctx)
				done.Swap(true)
			}()

			wg := sync.WaitGroup{}
			wg.Add(1)

			// The "Task" job will block a worker until the wait group is released.
			task := func(_ context.Context) (interface{}, error) {
				wg.Wait()
				return nil, nil
			}

			f1, err := pool.Enqueue(task)
			require.NoError(t, err)

			f2, err := pool.Enqueue(task)
			require.NoError(t, err)

			f3, err := pool.Enqueue(task)
			require.NoError(t, err)

			f4, err := pool.Enqueue(task)
			require.NoError(t, err)

			stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
			defer stopCancel()
			stopCalled := sync.WaitGroup{}
			stopCalled.Add(1)

			go func() {
				stopCalled.Done()
				pool.Stop(stopCtx)
			}()

			// Ensure the go routine is executing before proceeding.
			stopCalled.Wait()

			// all tasks should be blocked at this point.
			for _, future := range []Future{f1, f2, f3, f4} {
				_, timedout := executeFutureWithTimeout(t, future, 10*time.Millisecond)
				require.True(t, timedout)
			}

			// Ensure the workers are still running.
			consistently(t, func() bool {
				return !done.Load()
			}, 50*time.Millisecond)

			// Stop the waitGroup so it unblocks the tasks.
			wg.Done()

			// all tasks should be unblocked and finished at this point
			for _, future := range []Future{f1, f2, f3, f4} {
				_, timedout := executeFutureWithTimeout(t, future, 10*time.Millisecond)
				require.False(t, timedout)
			}

			require.Eventually(t, func() bool {
				return done.Load()
			}, 100*time.Millisecond, 10*time.Millisecond)
		})

		t.Run("Stop Timeout", func(t *testing.T) {
			pool := NewPool(2, 10)
			done := atomic.Bool{}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				pool.Work(ctx)
				done.Swap(true)
			}()

			wg := sync.WaitGroup{}
			wg.Add(1)
			defer wg.Done()

			// The "Task" job will block until the wait group is released.
			task := func(_ context.Context) (interface{}, error) {
				wg.Wait()
				return 100, nil
			}

			_, err := pool.Enqueue(task)
			require.NoError(t, err)

			stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Millisecond)
			defer stopCancel()

			stopExited := atomic.Bool{}
			go func() {
				pool.Stop(stopCtx)
				stopExited.Swap(true)
			}()

			// Eventually stop will exit even though there's a task blocking it.
			require.Eventually(t, func() bool {
				return stopExited.Load()
			}, 100*time.Millisecond, 10*time.Millisecond)

			// The pool won't be accepting new tasks
			_, err = pool.Enqueue(task)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrPoolNotRunning)
		})
	})

	t.Run("Using Context", func(t *testing.T) {
		pool := NewPool(2, 10)
		done := atomic.Bool{}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			pool.Work(ctx)
			done.Swap(true)
		}()

		cancel()

		require.Eventually(t, func() bool {
			return done.Load()
		}, time.Second, 10*time.Millisecond)

		t.Run("doesn't wait for pending tasks, but finish the ones already started", func(t *testing.T) {
			pool := NewPool(2, 10)
			done := atomic.Bool{}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				pool.Work(ctx)
				done.Swap(true)
			}()

			wg := sync.WaitGroup{}
			wg.Add(1)

			// The "Task" job will block a worker until the wait group is released.
			task := func(_ context.Context) (interface{}, error) {
				wg.Wait()
				return nil, nil
			}

			f1, err := pool.Enqueue(task)
			require.NoError(t, err)

			f2, err := pool.Enqueue(task)
			require.NoError(t, err)

			f3, err := pool.Enqueue(task)
			require.NoError(t, err)

			f4, err := pool.Enqueue(task)
			require.NoError(t, err)

			// All tasks should be blocked at this point.
			for _, future := range []Future{f1, f2, f3, f4} {
				_, timedout := executeFutureWithTimeout(t, future, 10*time.Millisecond)
				require.True(t, timedout)
			}

			// Cancel the worker context.
			cancel()

			// All tasks should be blocked at this point.
			// This is because f1 and f2 are already enqueued.
			for _, future := range []Future{f1, f2, f3, f4} {
				_, timedout := executeFutureWithTimeout(t, future, 10*time.Millisecond)
				require.True(t, timedout)
			}

			// Ensure the workers are still running.
			consistently(t, func() bool {
				return !done.Load()
			}, 50*time.Millisecond)

			// Stop the waitGroup so it unblocks the tasks.
			wg.Done()

			// f1 and f2 will finish running, because they were already being executed.
			_ = mustFutureWithTimeout(t, f1, 10*time.Millisecond)
			_ = mustFutureWithTimeout(t, f2, 10*time.Millisecond)

			// f3 and f4 will not get executed and fail
			_, err = futureWithTimeout(f3, 10*time.Millisecond)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrPoolNotRunning)
			_, err = futureWithTimeout(f4, 10*time.Millisecond)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrPoolNotRunning)

			require.Eventually(t, func() bool {
				return done.Load()
			}, 100*time.Millisecond, 10*time.Millisecond)
		})
	})
}

func Test_PoolResults(t *testing.T) {
	pool := NewPool(2, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go pool.Work(ctx)

	fString, err := pool.Enqueue(func(_ context.Context) (interface{}, error) {
		return "value", nil
	})
	require.NoError(t, err)

	fInt, err := pool.Enqueue(func(_ context.Context) (interface{}, error) {
		return 100, nil
	})
	require.NoError(t, err)

	result := mustFutureWithTimeout(t, fString, time.Millisecond)
	require.EqualValues(t, "value", result)

	result = mustFutureWithTimeout(t, fInt, time.Millisecond)
	require.EqualValues(t, 100, result)

	t.Run("Result() can be called multiple times with the same results", func(t *testing.T) {
		consistently(t, func() bool {
			result1 := mustFutureWithTimeout(t, fString, time.Millisecond)
			result2 := mustFutureWithTimeout(t, fInt, time.Millisecond)
			return result1.(string) == "value" && result2.(int) == 100

		}, 100*time.Millisecond)
	})

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()

	pool.Stop(stopCtx)

	t.Run("result context timeout", func(t *testing.T) {
		pool := NewPool(2, 10)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go pool.Work(ctx)

		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			pool.Stop(stopCtx)
		}()

		wg := sync.WaitGroup{}
		wg.Add(1)

		future, err := pool.Enqueue(func(_ context.Context) (interface{}, error) {
			wg.Wait()
			return "value", nil
		})
		require.NoError(t, err)

		resultCtx, resultCancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer resultCancel()

		_, err = future.Result(resultCtx)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrExternalContextCanceled)

		t.Run("Result() can be called again", func(t *testing.T) {
			// unblock the task
			wg.Done()

			resultCtx, resultCancel := context.WithTimeout(context.Background(), time.Millisecond)
			defer resultCancel()
			result, err := future.Result(resultCtx)
			require.NoError(t, err)
			require.EqualValues(t, "value", result)
		})
	})

	t.Run("parallel checking Result()", func(t *testing.T) {
		pool := NewPool(2, 10)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go pool.Work(ctx)

		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			pool.Stop(stopCtx)
		}()

		wg := sync.WaitGroup{}
		wg.Add(1)

		future, err := pool.Enqueue(func(_ context.Context) (interface{}, error) {
			wg.Wait()
			return "value", nil
		})
		require.NoError(t, err)

		resultCtx, resultCancel := context.WithCancel(context.Background())
		defer resultCancel()

		resultWG := sync.WaitGroup{}
		resultWG.Add(10)

		for i := 0; i < 10; i++ {
			go func() {
				result, err := future.Result(resultCtx)
				require.NoError(t, err)
				require.EqualValues(t, "value", result)
				resultWG.Done()
			}()
		}

		// unblock the task execution.
		wg.Done()

		// wait for all result go routines to finish.
		resultWG.Wait()
	})
}
