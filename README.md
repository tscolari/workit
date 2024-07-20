# workit

This is a worker pool implementation in Go. I've created as a way to exemplify some ideas.

It uses channels internally to control the list of tasks to perform,
the interface is very simple:

```
// New pool with 10 workers, and 1000 tasks queue/buffer size
pool := workit.NewPool(10, 1000)

// Start the workers, this function will block.
// Canceling the given context will cause it to eventually return.
go pool.Work(ctx)

// a Task is a simple function: func(context.Context) (interface{}, error)
// The context given to the Task is the same one given to Work(), so
// it's up to the implementation to listen to that or not during execution.
myTask := func(ctx context.Context) (interface{}, error) {
    ...
}

// If the task can't be enqueued (e.g. the pool has stopped) it will return an error.
// If the buffer is full, it will block until it can queue the task.
// It will return a future, that can be used to fetch the results from the Task
future, err := work.Enqueue(myTask)

// Result will block until the task is executed and returns.
// Canceling the given context here (e.g. Timeout) will cause
// Result to return before that with an ErrExternalContextCanceled error.
// Once a result is fetch, `Result()` will return that cached value
// as many times as it might be called.
result, err := future.Result(someCtx)

...

// Stop will cause the pool to finish, it will block until all already
// enqueued tasks are picked and finished - no new tasks will be accept by Enqueue.
// If the context given to Stop closes (e.g. Timeout) stop will not
// wait for the enqueued tasks to be picked up and finished - BUT already
// started Tasks will execute until the end.
pool.Stop(someCtx)

```

*Once a task is started, it will be finished*

Even if `Stop()` is called or the `Work()'s context` is closed, already
picked up tasks will execute to the end.
Tasks will be signaled by the context they receive, but it's up to the
implementation to define the behavior.
