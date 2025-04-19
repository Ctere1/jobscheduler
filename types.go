package jobscheduler

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// JobStatus represents the status of a job
type JobStatus string

const (
	StatusScheduled JobStatus = "scheduled"
	StatusRunning   JobStatus = "running"
	StatusSuccess   JobStatus = "success"
	StatusFailed    JobStatus = "failed"
)

// JobType represents the type of job
type JobType string

const (
	TypeFunction JobType = "function"
	TypeHTTP     JobType = "http"
)

// JobPayload contains the execution details of a job
type JobPayload struct {
	Type JobType        `json:"type"`
	Name string         `json:"name"`
	Func string         `json:"func,omitempty"`
	Data map[string]any `json:"data,omitempty"`
}

// Job represents a scheduled job
type Job struct {
	gorm.Model
	JobID        uuid.UUID  `gorm:"type:uuid;primaryKey"`
	Name         string     `gorm:"not null;size:255"`
	Spec         string     `gorm:"not null;size:100"`
	Payload      JobPayload `gorm:"type:jsonb"`
	Status       JobStatus  `gorm:"index;size:20;default:scheduled"`
	LastRun      *time.Time `gorm:"index"`
	NextRun      time.Time  `gorm:"index"`
	RunCount     int        `gorm:"default:0"`
	SuccessCount int        `gorm:"default:0"`
	FailCount    int        `gorm:"default:0"`
	LastError    string     `gorm:"type:text"`
	Latency      int64      `gorm:"default:0"` // milliseconds
}

// Scan implements the Scanner interface for JobPayload
func (jp *JobPayload) Scan(value any) error {
	if value == nil {
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal JobPayload: source is not bytes")
	}

	return json.Unmarshal(bytes, jp)
}

// Value implements the driver Valuer interface for JobPayload
func (jp JobPayload) Value() (driver.Value, error) {
	return json.Marshal(jp)
}
