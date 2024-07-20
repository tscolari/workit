package workit

import "errors"

var (
	// ErrPoolNotRunning means the pool is no longer running and any
	// task that was enqueued (or is trying to be enqueued) won't run.
	ErrPoolNotRunning = errors.New("the pool is no longer running")

	// ErrExternalContextCanceled means the context given by the caller
	// of the function was canceled.
	// This prompted the function to exit.
	ErrExternalContextCanceled = errors.New("the caller context was canceled")
)
