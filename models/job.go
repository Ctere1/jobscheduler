package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

type JobStatus string

const (
	StatusScheduled JobStatus = "scheduled"
	StatusRunning   JobStatus = "running"
	StatusSuccess   JobStatus = "success"
	StatusFailed    JobStatus = "failed"
)

type JobType string

const (
	TypeFunction JobType = "function"
	TypeCommand  JobType = "command"
)

type JobPayload struct {
	Type JobType `json:"type"`
	Name string  `json:"name"`
	Func string  `json:"func"`
	Data any     `json:"data,omitempty"`
}

type Job struct {
	gorm.Model
	JobID        string     `gorm:"uniqueIndex;not null;size:36"`
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

type JSONB map[string]any

func (jp JobPayload) Value() (driver.Value, error) {
	return json.Marshal(jp)
}

func (jp *JobPayload) Scan(value any) error {
	b, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(b, jp)
}
