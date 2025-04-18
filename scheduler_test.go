package jobscheduler

import (
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ctere1/jobscheduler/models"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using default environment")
	}
	os.Exit(m.Run())
}

// Test database setup
func setupTestDB(t *testing.T) *gorm.DB {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC",
		os.Getenv("PG_HOST"),
		os.Getenv("PG_USER"),
		os.Getenv("PG_PASSWORD"),
		os.Getenv("PG_DBNAME"),
		os.Getenv("PG_PORT"),
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Clean up and migrate
	db.Exec("DROP TABLE IF EXISTS jobs")
	if err := db.AutoMigrate(&models.Job{}); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	return db
}

func TestConcurrentJobExecution(t *testing.T) {
	db := setupTestDB(t)

	scheduler, err := New(db, Config{
		MaxConcurrentJobs: 2,
		JobTimeout:        time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		runningJobs int
		maxRunning  int
	)

	// Create a function that tracks concurrent execution
	trackFunc := func() {
		mu.Lock()
		runningJobs++
		if runningJobs > maxRunning {
			maxRunning = runningJobs
		}
		mu.Unlock()

		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		runningJobs--
		mu.Unlock()
		wg.Done()
	}

	// Schedule 4 jobs
	for i := 0; i < 4; i++ {
		wg.Add(1)
		job := &models.Job{
			JobID: fmt.Sprintf("concurrent-job-%d", i),
			Payload: models.JobPayload{
				Type: models.TypeFunction,
				Func: fmt.Sprintf("func%d", i),
			},
		}

		// Register and execute
		scheduler.RegisterFunction(job.Payload.Func, trackFunc)
		scheduler.createJobFunc(job)()
	}

	wg.Wait()

	// Verify max concurrency was respected
	if maxRunning > 2 {
		t.Errorf("Expected max 2 concurrent jobs, got %d", maxRunning)
	}
}

func TestRestoreJobs(t *testing.T) {
	db := setupTestDB(t)

	scheduler, err := New(db, Config{})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	// Create a test job directly in the database
	testJob := &models.Job{
		JobID: "restore-job",
		Name:  "restore-test",
		Spec:  "* * * * *",
		Payload: models.JobPayload{
			Type: models.TypeFunction,
			Func: "restoreFunc",
		},
		Status: models.StatusScheduled,
	}

	if err := db.Create(testJob).Error; err != nil {
		t.Fatalf("Failed to create test job: %v", err)
	}

	scheduler.restoreJobs()

	if len(scheduler.jobMap) == 0 {
		t.Errorf("Expected job to be restored, but no jobs found")
	}
}

func TestFunctionRegistration(t *testing.T) {
	db := setupTestDB(t)

	scheduler, err := New(db, Config{})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	testFunc := func() {}

	funcName := runtime.FuncForPC(reflect.ValueOf(testFunc).Pointer()).Name()
	scheduler.RegisterFunction(funcName, testFunc)

	// Verify function was registered
	scheduler.funcMu.RLock()
	_, ok := scheduler.funcMap[funcName]
	scheduler.funcMu.RUnlock()

	if !ok {
		t.Errorf("Function was not registered properly")
	}
}

func TestJobExecution(t *testing.T) {
	db := setupTestDB(t)

	scheduler, err := New(db, Config{
		MaxConcurrentJobs: 1,
	})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	var jobRan bool

	testFunc := func() {
		jobRan = true
	}

	job := &models.Job{
		JobID: "test-job",
		Payload: models.JobPayload{
			Type: models.TypeFunction,
			Func: "testFunc",
		},
	}

	scheduler.RegisterFunction(job.Payload.Func, testFunc)
	scheduler.createJobFunc(job)()

	if !jobRan {
		t.Error("Expected job to run")
	}
}

func TestCronScheduleAccuracy(t *testing.T) {
	db := setupTestDB(t)

	scheduler, err := New(db, Config{
		MaxConcurrentJobs: 1,
		JobTimeout:        time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	var (
		wg           sync.WaitGroup
		runTimes     []time.Time
		mu           sync.Mutex
		expectedRuns = 3
	)

	testFunc := func() {
		mu.Lock()
		runTimes = append(runTimes, time.Now())
		mu.Unlock()
		wg.Done()
	}

	// Schedule job to run every second
	wg.Add(expectedRuns)
	_, err = scheduler.Schedule("cron-accuracy-test", "@every 1s", testFunc, nil)
	if err != nil {
		t.Fatalf("Failed to schedule job: %v", err)
	}

	// Wait for expected runs (with buffer time)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Duration(expectedRuns+2) * time.Second):
		t.Errorf("Expected %d runs, got %d", expectedRuns, len(runTimes))
	}

	// Verify timing between runs (should be ~1 second apart)
	for i := 1; i < len(runTimes); i++ {
		diff := runTimes[i].Sub(runTimes[i-1])
		if diff < 900*time.Millisecond || diff > 1100*time.Millisecond {
			t.Errorf("Expected ~1s between runs, got %v between run %d and %d", diff, i-1, i)
		}
	}
}

func TestLongRunningJobHandling(t *testing.T) {
	db := setupTestDB(t)

	scheduler, err := New(db, Config{
		MaxConcurrentJobs: 1,
		JobTimeout:        500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	var (
		jobRan      bool
		jobFinished bool
	)

	longFunc := func() {
		jobRan = true
		time.Sleep(2 * time.Second)
		jobFinished = true
	}

	job := &models.Job{
		JobID: "long-running-job",
		Payload: models.JobPayload{
			Type: models.TypeFunction,
			Func: "longFunc",
		},
	}

	scheduler.RegisterFunction(job.Payload.Func, longFunc)
	scheduler.createJobFunc(job)()

	// Verify job ran but didn't finish
	if !jobRan {
		t.Error("Expected job to start running")
	}
	if jobFinished {
		t.Error("Expected job to be interrupted by timeout")
	}

	// Check database status
	var dbJob models.Job
	if err := db.First(&dbJob, "job_id = ?", job.JobID).Error; err != nil {
		t.Fatalf("Failed to find job: %v", err)
	}

	if dbJob.Status != models.StatusFailed {
		t.Errorf("Expected job status 'failed', got '%s'", dbJob.Status)
	}
	if dbJob.LastError != ErrJobTimeout.Error() {
		t.Errorf("Expected timeout error, got '%s'", dbJob.LastError)
	}
}

func TestMultipleJobManagement(t *testing.T) {
	db := setupTestDB(t)

	scheduler, err := New(db, Config{
		MaxConcurrentJobs: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	// Schedule 50 jobs
	var jobs []*models.Job
	for i := 0; i < 50; i++ {
		testFunc := func() {}

		job, err := scheduler.Schedule(fmt.Sprintf("multi-job-%d", i), fmt.Sprintf("%d * * * *", i%60), testFunc, nil)
		if err != nil {
			t.Fatalf("Failed to schedule job %d: %v", i, err)
		}
		jobs = append(jobs, job)
	}

	// Verify all jobs are scheduled
	dbJobs, err := scheduler.ListJobs()
	if err != nil {
		t.Fatalf("Failed to list jobs: %v", err)
	}
	if len(dbJobs) != 50 {
		t.Errorf("Expected 50 jobs, got %d", len(dbJobs))
	}

	// Unschedule half of them
	for i := 0; i < 25; i++ {
		if err := scheduler.Unschedule(jobs[i].JobID); err != nil {
			t.Fatalf("Failed to unschedule job %s: %v", jobs[i].JobID, err)
		}
	}

	// Verify remaining jobs
	dbJobs, err = scheduler.ListJobs()
	if err != nil {
		t.Fatalf("Failed to list jobs: %v", err)
	}
	if len(dbJobs) != 25 {
		t.Errorf("Expected 25 jobs after unscheduling, got %d", len(dbJobs))
	}
}

func TestPanicRecovery(t *testing.T) {
	db := setupTestDB(t)

	scheduler, err := New(db, Config{})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	panicFunc := func() {
		panic("simulated panic")
	}

	job := &models.Job{
		JobID: "panic-job",
		Payload: models.JobPayload{
			Type: models.TypeFunction,
			Func: "panicFunc",
		},
	}

	scheduler.RegisterFunction(job.Payload.Func, panicFunc)
	scheduler.createJobFunc(job)()

	// Verify job was marked as failed
	var dbJob models.Job
	if err := db.First(&dbJob, "job_id = ?", job.JobID).Error; err != nil {
		t.Fatalf("Failed to find job: %v", err)
	}

	if dbJob.Status != models.StatusFailed {
		t.Errorf("Expected job status 'failed', got '%s'", dbJob.Status)
	}
	if !strings.Contains(dbJob.LastError, "simulated panic") {
		t.Errorf("Expected panic error, got '%s'", dbJob.LastError)
	}
}

func TestJobUnscheduling(t *testing.T) {
	db := setupTestDB(t)

	scheduler, err := New(db, Config{})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	testFunc := func() {}

	job, err := scheduler.Schedule("unschedule-job", "* * * * *", testFunc, nil)
	if err != nil {
		t.Fatalf("Failed to schedule job: %v", err)
	}

	// Verify job exists in database
	var dbJob models.Job
	if err := db.First(&dbJob, "job_id = ?", job.JobID).Error; err != nil {
		t.Fatalf("Failed to find job: %v", err)
	}

	// Unschedule the job
	if err := scheduler.Unschedule(job.JobID); err != nil {
		t.Fatalf("Failed to unschedule job: %v", err)
	}

	// Verify job no longer exists in database
	if err := db.First(&dbJob, "job_id = ?", job.JobID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("Expected job to be unscheduled, but it still exists: %v", err)
	}
}

func TestJobList(t *testing.T) {
	db := setupTestDB(t)

	scheduler, err := New(db, Config{})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	testFunc := func() {}

	// Schedule multiple jobs
	for i := 0; i < 5; i++ {
		job, err := scheduler.Schedule(fmt.Sprintf("list-job-%d", i), "* * * * *", testFunc, nil)
		if err != nil {
			t.Fatalf("Failed to schedule job %d: %v", i, err)
		}

		// Verify job exists in database
		var dbJob models.Job
		if err := db.First(&dbJob, "job_id = ?", job.JobID).Error; err != nil {
			t.Fatalf("Failed to find job %d: %v", i, err)
		}
	}

	// List jobs
	jobs, err := scheduler.ListJobs()
	if err != nil {
		t.Fatalf("Failed to list jobs: %v", err)
	}

	if len(jobs) != 5 {
		t.Errorf("Expected 5 jobs, got %d", len(jobs))
	}
}

func TestPostgresSpecificFeatures(t *testing.T) {
	db := setupTestDB(t)

	t.Run("TestTransactionRollbackOnFailure", func(t *testing.T) {
		// Mock a function that will fail when called
		mockFunc := func() {
			panic("simulated failure")
		}

		scheduler, err := New(db, Config{})
		if err != nil {
			t.Fatalf("Failed to create scheduler: %v", err)
		}
		defer scheduler.Stop()

		// Schedule job
		_, err = scheduler.Schedule("failing-job", "* * * * *", mockFunc, nil)
		if err != nil {
			t.Fatalf("Failed to schedule job: %v", err)
		}

		// Get the job from database
		var job models.Job
		if err := db.Where("name = ?", "failing-job").First(&job).Error; err != nil {
			t.Fatalf("Failed to find job: %v", err)
		}

		// Execute the job
		scheduler.createJobFunc(&job)()

		// Verify job status was updated to failed
		if err := db.First(&job, "job_id = ?", job.JobID).Error; err != nil {
			t.Fatalf("Failed to find job: %v", err)
		}

		if job.Status != models.StatusFailed {
			t.Errorf("Expected job status 'failed', got '%s'", job.Status)
		}

		if job.LastError == "" {
			t.Error("Expected error message to be recorded")
		}
	})

	t.Run("TestJSONBFieldOperations", func(t *testing.T) {
		scheduler, err := New(db, Config{})
		if err != nil {
			t.Fatalf("Failed to create scheduler: %v", err)
		}
		defer scheduler.Stop()

		complexMetadata := map[string]any{
			"string": "value",
			"number": 42,
			"nested": map[string]any{
				"array":  []any{1, 2, 3},
				"bool":   true,
				"null":   nil,
				"struct": struct{ Field string }{Field: "test"},
			},
		}

		testFunc := func() {}
		job, err := scheduler.Schedule("jsonb-test", "* * * * *", testFunc, complexMetadata)
		if err != nil {
			t.Fatalf("Failed to schedule job: %v", err)
		}

		// Verify JSONB data was stored correctly
		var dbJob models.Job
		if err := db.First(&dbJob, "job_id = ?", job.JobID).Error; err != nil {
			t.Fatalf("Failed to find job: %v", err)
		}

		// Fix: Directly access the data as map[string]interface{}
		payloadData, ok := dbJob.Payload.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected payload data to be map[string]interface{}, got %T", dbJob.Payload.Data)
		}

		// Verify nested data
		nested, ok := payloadData["nested"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected nested data to be map[string]interface{}")
		}

		if nested["bool"] != true {
			t.Errorf("Expected nested bool true, got %v", nested["bool"])
		}

		// Additional verifications
		if payloadData["string"] != "value" {
			t.Errorf("Expected string 'value', got '%v'", payloadData["string"])
		}

		if payloadData["number"] != float64(42) { // JSON numbers decode as float64
			t.Errorf("Expected number 42, got %v", payloadData["number"])
		}
	})

	t.Run("TestDatabaseConnectionLossHandling", func(t *testing.T) {
		// Setup - create a job that should persist
		db := setupTestDB(t)
		scheduler, err := New(db, Config{})
		if err != nil {
			t.Fatalf("Failed to create scheduler: %v", err)
		}
		defer scheduler.Stop()

		testFunc := func() {}
		job, err := scheduler.Schedule("connection-test", "* * * * *", testFunc, nil)
		if err != nil {
			t.Fatalf("Failed to schedule job: %v", err)
		}

		// Verify job exists in database
		var dbJob models.Job
		if err := db.First(&dbJob, "job_id = ?", job.JobID).Error; err != nil {
			t.Fatalf("Job should exist before connection loss test: %v", err)
		}

		// Mock a database failure without actually closing the connection
		oldDB := scheduler.db
		defer func() { scheduler.db = oldDB }() // Restore after test

		// Create a mock DB that will fail
		failDB, err := gorm.Open(postgres.Open("host=invalid dbname=invalid"), &gorm.Config{})
		if err == nil {
			t.Fatal("Expected error creating invalid DB connection")
		}

		// Replace scheduler's DB with failing one
		scheduler.db = failDB

		// Attempt unschedule - should fail
		err = scheduler.Unschedule(job.JobID)
		if err == nil {
			t.Error("Expected error when database is failing, got nil")
		}

		// Restore working connection
		scheduler.db = oldDB

		// Verify job still exists
		if err := db.First(&dbJob, "job_id = ?", job.JobID).Error; err != nil {
			t.Fatalf("Job should still exist after connection restored: %v", err)
		}
	})
}
