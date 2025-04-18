package jobscheduler

import "errors"

var (
	ErrInvalidJobSpec       = errors.New("invalid job specification")
	ErrJobNotFound          = errors.New("job not found")
	ErrJobAlreadyExists     = errors.New("job already exists")
	ErrJobTimeout           = errors.New("job execution timed out")
	ErrFunctionNotFound     = errors.New("function not registered")
	ErrJobNameEmpty         = errors.New("job name cannot be empty")
	ErrJobFunctionNil       = errors.New("job function cannot be nil")
	ErrJobFunctionInvalid   = errors.New("provided value is not a function")
	ErrJobFunctionNameEmpty = errors.New("could not determine function name")
)
