package jobscheduler

import (
	"context"

	"github.com/google/uuid"
)

// Storage defines the interface for job persistence
type Storage interface {
	CreateJob(ctx context.Context, job *Job) error
	UpdateJob(ctx context.Context, job *Job) error
	DeleteJob(ctx context.Context, uuid uuid.UUID) error
	GetJobByJobID(ctx context.Context, uuid uuid.UUID) (*Job, error)
	GetJobByName(ctx context.Context, name string) (*Job, error)
	ListJobs(ctx context.Context) ([]Job, error)
}
