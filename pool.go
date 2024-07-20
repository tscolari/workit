package workit

import (
	"context"
	"fmt"
	"sync"
)

// Task is a unit of work to be performed by the Pool.
// The context given to the Task function will be the same
// that was used to call `pool.Task`.
type Task func(context.Context) (interface{}, error)

// Future is returned with Work is scheduled with the Pool.
// The future can be used to check the result of that unit
// of work, when it's done.
type Future interface {
	// Result will block until the Work is finished.
	// Then it will return either the return the result value or an error,
	// depending on the execution.
	//
	// Result is not aware of the Pool termination, so in the case
	// the pool gets terminated before the Work is finished or picked up,
	// it will block indefinitely.
	Result(context.Context) (interface{}, error)
}

// Pool provides an interface for scheduling work, starting the work,
// and eventually stopping all work.
type Pool interface {
	// Enqueue adds a Task to the Pool queue and return a Future for
	// result verification by the caller.
	// If the Task can't be enqueued, then an error is returned.
	// If the pool's task buffer is full, Enqueue will block until
	// some space is made for this Task.
	Enqueue(Task) (Future, error)

	// Work will run all schedules of work and block until either the context is canceled
	// or the Pool.Stop() method is called.
	// If either the context is called, no new tasks will be picked up,
	// but already executing ones will continue to run - Work() only unblocks
	// when no more tasks are actively running.
	Work(context.Context)

	// Stop will force the Pool to stop. Once stopped it can no longer be restarted.
	// Calling Stop will immediately prevent new tasks from being enqueued.
	// Stop will block until all already enqueued tasks are executed, but if the
	// context given to it closes - it will accelerate the process by not allowing
	// any new task from being started (only already picked up ones will finish execution).
	Stop(context.Context)
}

// NewPool will return a new Pool object with the size configuration given.
func NewPool(poolSize, bufferSize int) Pool {
	runningCtx, cancelFunc := context.WithCancel(context.Background())

	return &pool{
		pendingTasks:  make(chan *task, bufferSize),
		poolSize:      poolSize,
		runningCtx:    runningCtx,
		runningCtxEnd: cancelFunc,
	}
}

// pool is the implementation of the Pool interface.
type pool struct {
	// pendingTasks keep track of all enqueued tasks that haven't been
	// picked up for execution yet.
	pendingTasks chan *task

	// poolSize defines how many "workers" / go routines the pool
	// have picking up tasks and executing them.
	poolSize int

	// runningCtx is an internal context that tracks if the pool is
	// still running. It is shared with the Future of all enqueued
	// tasks so that they can notify the caller of Result() that
	// the task won't be executed.
	runningCtx context.Context

	// runningCtxEnd is the function that cancels the internal runningCtx.
	runningCtxEnd func()
}

// Enqueue will schedule a unit of work in the Pool and return a Future
// object so that the caller can check the results.
func (p *pool) Enqueue(work Task) (Future, error) {

	select {
	case <-p.runningCtx.Done():
		return nil, fmt.Errorf("can't enqueue new tasks: %w", ErrPoolNotRunning)

	default:
		workTask := task{
			taskFunc:   work,
			resultChan: make(chan *result),
			poolCtx:    p.runningCtx,
		}

		p.pendingTasks <- &workTask
		return &workTask, nil
	}
}

// Work will block until the context is canceled or the Stop method is called.
// While running, work will listen to all units of Work queued and
// distribute them across `poolSize` go routines to perform them.
func (p *pool) Work(ctx context.Context) {
	// When Work exits, the internal context must be canceled.
	// This is a way to signal to all pending tasks that they won't be performed,
	// and allow each returned Future to exit with a proper error instead of block.
	defer p.runningCtxEnd()

	wg := sync.WaitGroup{}

	for i := 0; i < p.poolSize; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				select {
				// If the internal context is canceled, it means we received a request to Stop.
				// And that we should not drain the pendingTasks anymore, so no new task will
				// be picked up.
				case <-p.runningCtx.Done():
					return

				// If the caller context is canceled, the worker will no longer look for
				// new tasks and exit.
				case <-ctx.Done():
					return

				case workTask, ok := <-p.pendingTasks:
					if !ok {
						return
					}

					// Another check on the contexts, because we cant guarantee
					// the order in the parent select, and we dont want to start
					// any new task if they are closed.
					select {
					case <-ctx.Done():
						return
					case <-p.runningCtx.Done():
						return

					default:
						value, err := workTask.taskFunc(ctx)
						workTask.resultChan <- &result{
							value: value,
							err:   err,
						}
					}

				}
			}
		}()
	}

	wg.Wait()
}

// Stop will close the internal Task channel, preventing any new task from being scheduled.
// The pool will keep processing tasks that were already scheduled, until the given context
// is canceled (e.g. timeout) or the queue is empty.
// Stop will return once no new tasks will be picked up.
func (p *pool) Stop(ctx context.Context) {
	close(p.pendingTasks)

	select {
	// If the caller context is done, finish the internal context
	// so that the workers exit as soon as possible.
	case <-ctx.Done():
		p.runningCtxEnd()

	// If the internal context is done, it means the workers have finished
	// running all pending tasks, and Stop is now complete.
	case <-p.runningCtx.Done():
		return
	}
}

// task is an internal representation of a work unit to be performed.
type task struct {
	taskFunc   Task
	resultChan chan *result
	result     *result

	poolCtx context.Context
}

type result struct {
	value interface{}
	err   error
}

// Result is a blocking function that will block until the respective
// unit of work is finished.
func (t *task) Result(ctx context.Context) (interface{}, error) {
	if t.result != nil {
		return t.result.value, t.result.err
	}

	select {
	case result, ok := <-t.resultChan:
		if !ok {
			// resultChan can only be closed if another call to `Result` fetched the values.
			// In that case it's safe to break from the select and just read from the
			// result stored in the task object.
			break
		}

		// caches the result so that other calls to Result() can use.
		t.result = result
		close(t.resultChan)

	// When the context of the caller of Result is closed, this will stop waiting for the result.
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for result canceled: %w", ErrExternalContextCanceled)

	// When the pool finishes, it closes this context that is shared with all tasks.
	// If anyone waiting for this Result gets to this point, it means that the job
	// wasn't ever started nor it will be.
	case <-t.poolCtx.Done():
		t.result = &result{
			err: fmt.Errorf("task not executed: %w", ErrPoolNotRunning),
		}
	}

	return t.result.value, t.result.err
}
