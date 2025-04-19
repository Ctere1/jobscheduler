package jobscheduler

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// gormStorage implements Storage interface using GORM
type gormStorage struct {
	db *gorm.DB
}

// newGormStorage creates a new GORM-based storage
func newGormStorage(db *gorm.DB) *gormStorage {
	return &gormStorage{db: db}
}

// CreateJob persists a new job
func (s *gormStorage) CreateJob(ctx context.Context, job *Job) error {
	return s.db.WithContext(ctx).Create(job).Error
}

// UpdateJob updates an existing job
func (s *gormStorage) UpdateJob(ctx context.Context, job *Job) error {
	result := s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", job.ID).Updates(job)
	if result.Error != nil {
		return result.Error
	}

	// Check if any rows were actually updated
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}

	return nil
}

// DeleteJob removes a job
func (s *gormStorage) DeleteJob(ctx context.Context, uuid uuid.UUID) error {
	return s.db.WithContext(ctx).Where("job_id = ?", uuid).Delete(&Job{}).Error
}

// GetJobByID retrieves a job by its JobID
func (s *gormStorage) GetJobByJobID(ctx context.Context, uuid uuid.UUID) (*Job, error) {
	var job Job
	err := s.db.WithContext(ctx).Where("job_id = ?", uuid).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrJobNotFound
	}
	return &job, err
}

// GetJobByName retrieves a job by its name
func (s *gormStorage) GetJobByName(ctx context.Context, name string) (*Job, error) {
	var job Job
	err := s.db.WithContext(ctx).Where("name = ?", name).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrJobNotFound
	}
	return &job, err
}

// ListJobs returns all jobs ordered by name
func (s *gormStorage) ListJobs(ctx context.Context) ([]Job, error) {
	var jobs []Job
	err := s.db.WithContext(ctx).Order("name ASC").Find(&jobs).Error
	return jobs, err
}
