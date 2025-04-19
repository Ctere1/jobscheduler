package jobscheduler_test

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Ctere1/jobscheduler"
)

func setupTestScheduler(t *testing.T) *jobscheduler.Scheduler {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC",
		os.Getenv("PG_HOST"),
		os.Getenv("PG_USER"),
		os.Getenv("PG_PASSWORD"),
		os.Getenv("PG_DBNAME"),
		os.Getenv("PG_PORT"),
	)
	if dsn == "" {
		t.Skip("Skipping test: PG_HOST, PG_USER, PG_PASSWORD, PG_DBNAME, and PG_PORT must be set")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	// Clean slate
	_ = db.Exec("DROP TABLE IF EXISTS jobs")
	err = db.AutoMigrate(&jobscheduler.Job{})
	require.NoError(t, err)

	scheduler, err := jobscheduler.New(db, jobscheduler.Config{
		MaxConcurrentJobs: 3, // test concurrency limit here
		JobTimeout:        2 * time.Second,
	})
	assert.NoError(t, err)

	return scheduler
}

func sampleJob() {
	log.Println("sampleJob executed")
}

func TestScheduleJob(t *testing.T) {
	s := setupTestScheduler(t)

	job, err := s.Schedule("test-job", "@every 1s", sampleJob, map[string]any{"env": "test"})
	assert.NoError(t, err)
	assert.NotNil(t, job)
	assert.Equal(t, "test-job", job.Name)

	jobs, err := s.ListJobs()
	assert.NoError(t, err)
	assert.Len(t, jobs, 1)
}

func TestRescheduleJob(t *testing.T) {
	s := setupTestScheduler(t)

	_, err := s.Schedule("reschedulable", "@every 5s", sampleJob, nil)
	assert.NoError(t, err)

	updated, err := s.Reschedule("reschedulable", "@every 10s", map[string]any{"key": "val"})
	assert.NoError(t, err)
	assert.Equal(t, "@every 10s", updated.Spec)
	assert.Equal(t, "val", updated.Payload.Data["key"])
}

func TestRunNow(t *testing.T) {
	s := setupTestScheduler(t)

	_, err := s.Schedule("immediate", "@every 10m", sampleJob, nil)
	assert.NoError(t, err)

	err = s.RunNow("immediate")
	assert.NoError(t, err)

	time.Sleep(300 * time.Millisecond)

	job, err := s.GetJob("immediate")
	assert.NoError(t, err)
	assert.Equal(t, jobscheduler.StatusSuccess, job.Status)
	assert.NotNil(t, job.LastRun)
}

func TestUnscheduleJob(t *testing.T) {
	s := setupTestScheduler(t)

	_, err := s.Schedule("removable", "@every 1h", sampleJob, nil)
	assert.NoError(t, err)

	err = s.Unschedule("removable")
	assert.NoError(t, err)

	_, err = s.GetJob("removable")
	assert.Error(t, err)
}

func TestGetJobByID(t *testing.T) {
	s := setupTestScheduler(t)

	job, err := s.Schedule("by-id", "@every 2h", sampleJob, nil)
	assert.NoError(t, err)

	got, err := s.GetJob(job.JobID)
	assert.NoError(t, err)
	assert.Equal(t, job.Name, got.Name)
}

func TestConcurrencyLimit(t *testing.T) {
	s := setupTestScheduler(t)

	blockingJob := func() {
		time.Sleep(1 * time.Second)
	}

	j1, err := s.Schedule("job1", "@every 1m", blockingJob, nil)
	assert.NoError(t, err)
	j2, err := s.Schedule("job2", "@every 1m", blockingJob, nil)
	assert.NoError(t, err)
	j3, err := s.Schedule("job3", "@every 1m", blockingJob, nil)
	assert.NoError(t, err)

	err = s.RunNow(j1.JobID)
	assert.NoError(t, err)
	err = s.RunNow(j2.JobID)
	assert.NoError(t, err)

	err = s.RunNow(j3.JobID)
	assert.ErrorIs(t, err, jobscheduler.ErrConcurrencyLimit)
}
