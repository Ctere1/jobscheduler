package jobscheduler

import "errors"

// Error definitions for the job scheduler
var (
	ErrJobNameEmpty         = errors.New("job name cannot be empty")
	ErrJobFunctionNil       = errors.New("job function cannot be nil")
	ErrJobFunctionInvalid   = errors.New("job function must be a valid function")
	ErrJobFunctionNameEmpty = errors.New("could not determine function name")
	ErrJobNotFound          = errors.New("job not found")
	ErrFunctionNotFound     = errors.New("function not registered")
	ErrJobTimeout           = errors.New("job execution timed out")
	ErrJobAlreadyRunning    = errors.New("job is already running")
	ErrConcurrencyLimit     = errors.New("concurrency limit reached")
)
