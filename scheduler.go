package jobscheduler

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// Scheduler manages job scheduling and execution
type Scheduler struct {
	cron      *cron.Cron
	storage   Storage
	config    Config
	semaphore chan struct{}
	funcMap   map[string]any
	funcMu    sync.RWMutex
	jobLocks  map[uint]*sync.Mutex
	jobLockMu sync.Mutex
	jobMap    map[cron.EntryID]uint
	mapMutex  sync.RWMutex
	webUI     *WebUI
}

// New creates a new Scheduler instance
func New(db *gorm.DB, config Config) (*Scheduler, error) {
	// Apply default values if not set
	if config.MaxConcurrentJobs <= 0 {
		config.MaxConcurrentJobs = DefaultMaxConcurrentJobs
	}
	if config.JobTimeout <= 0 {
		config.JobTimeout = DefaultJobTimeout
	}

	// Initialize storage
	storage := newGormStorage(db)
	if err := db.AutoMigrate(&Job{}); err != nil {
		return nil, err
	}

	// Create scheduler instance
	s := &Scheduler{
		cron:      cron.New(cron.WithChain(cron.Recover(cron.DefaultLogger))),
		storage:   storage,
		config:    config,
		semaphore: make(chan struct{}, config.MaxConcurrentJobs),
		funcMap:   make(map[string]any),
		jobLocks:  make(map[uint]*sync.Mutex),
		jobMap:    make(map[cron.EntryID]uint),
	}

	// Restore existing jobs from storage
	if err := s.restoreJobs(); err != nil {
		return nil, err
	}

	// Start the cron scheduler
	s.cron.Start()

	// Start web UI if enabled
	if config.EnableWebUI {
		s.webUI = NewWebUI(s)
		go s.webUI.Start(config.WebListen)
	}

	return s, nil
}

// Schedule creates and schedules a new job
func (s *Scheduler) Schedule(name, spec string, function any, metadata map[string]any) (*Job, error) {
	// Input validation
	if name == "" {
		return nil, ErrJobNameEmpty
	}
	if function == nil {
		return nil, ErrJobFunctionNil
	}

	// Validate cron spec
	if _, err := cron.ParseStandard(spec); err != nil {
		return nil, err
	}

	// Get function info
	funcValue := reflect.ValueOf(function)
	if funcValue.Kind() != reflect.Func {
		return nil, ErrJobFunctionInvalid
	}

	funcName := runtime.FuncForPC(funcValue.Pointer()).Name()
	if funcName == "" {
		return nil, ErrJobFunctionNameEmpty
	}

	// Register function
	s.registerFunction(funcName, function)

	// Check for existing job with same name
	existingJob, err := s.getJobByIdentifier(name)
	if err == nil {
		oldFuncName := existingJob.Payload.Func
		if oldFuncName != funcName {
			// Reschedule with new function
			updatedJob, err := s.Reschedule(existingJob.JobID, spec, metadata)
			if err == nil {
				s.funcMu.Lock()
				delete(s.funcMap, oldFuncName)
				s.funcMu.Unlock()
			}
			return updatedJob, err
		}
		return s.Reschedule(existingJob.JobID, spec, metadata)
	}

	// Create new job
	job := &Job{
		JobID: uuid.New(),
		Name:  name,
		Spec:  spec,
		Payload: JobPayload{
			Type: TypeFunction,
			Name: name,
			Func: funcName,
			Data: metadata,
		},
		Status: StatusScheduled,
	}

	if err := s.storage.CreateJob(context.Background(), job); err != nil {
		return nil, err
	}

	if err := s.scheduleJob(job); err != nil {
		s.storage.DeleteJob(context.Background(), job.JobID)
		return nil, err
	}

	return job, nil
}

// Reschedule updates an existing job
func (s *Scheduler) Reschedule(identifier any, newSpec string, newMetadata map[string]any) (*Job, error) {
	job, err := s.getJobByIdentifier(identifier)
	if err != nil {
		return nil, err
	}

	// Validate new cron spec if changed
	if newSpec != "" {
		if _, err := cron.ParseStandard(newSpec); err != nil {
			return nil, err
		}
		job.Spec = newSpec
	}

	// Update metadata if provided
	if newMetadata != nil {
		job.Payload.Data = newMetadata
	}

	// Remove existing schedule
	s.removeJobFromCron(job.ID)

	// Add new schedule
	if err := s.scheduleJob(job); err != nil {
		return nil, err
	}

	// Save updates
	if err := s.storage.UpdateJob(context.Background(), job); err != nil {
		return nil, err
	}

	return job, nil
}

// Unschedule removes a job
func (s *Scheduler) Unschedule(identifier any) error {
	job, err := s.getJobByIdentifier(identifier)
	if err != nil {
		return err
	}

	s.removeJobFromCron(job.ID)

	s.funcMu.Lock()
	delete(s.funcMap, job.Payload.Func)
	s.funcMu.Unlock()

	return s.storage.DeleteJob(context.Background(), job.JobID)
}

// RunNow executes a job immediately
func (s *Scheduler) RunNow(identifier any) error {
	job, err := s.getJobByIdentifier(identifier)
	if err != nil {
		return err
	}

	jobLock := s.getJobLock(job.ID)
	jobLock.Lock()
	defer jobLock.Unlock()

	if job.Status == StatusRunning {
		return ErrJobAlreadyRunning
	}

	select {
	case s.semaphore <- struct{}{}:
		go func() {
			defer func() { <-s.semaphore }()
			s.createJobFunc(job)()
		}()
		return nil
	default:
		return ErrConcurrencyLimit
	}
}

// GetJob retrieves a job by ID or name
func (s *Scheduler) GetJob(identifier any) (*Job, error) {
	return s.getJobByIdentifier(identifier)
}

// ListJobs returns all jobs
func (s *Scheduler) ListJobs() ([]Job, error) {
	return s.storage.ListJobs(context.Background())
}

// Stop shuts down the scheduler
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// Private helper methods

func (s *Scheduler) getJobByIdentifier(identifier any) (*Job, error) {
	switch v := identifier.(type) {
	case string:
		// Try to parse as UUID first (job ID)
		if uuid, err := uuid.Parse(v); err == nil {
			return s.storage.GetJobByJobID(context.Background(), uuid)
		}
		return s.storage.GetJobByName(context.Background(), v)
	case uuid.UUID:
		return s.storage.GetJobByJobID(context.Background(), v)
	default:
		return nil, ErrJobNotFound
	}
}

func (s *Scheduler) registerFunction(name string, fn any) {
	s.funcMu.Lock()
	defer s.funcMu.Unlock()
	s.funcMap[name] = fn
}

func (s *Scheduler) restoreJobs() error {
	jobs, err := s.storage.ListJobs(context.Background())
	if err != nil {
		return err
	}

	seen := make(map[string]bool)
	for _, job := range jobs {
		if seen[job.Name] {
			continue
		}
		seen[job.Name] = true

		if err := s.scheduleJob(&job); err != nil {
			continue
		}
	}
	return nil
}

func (s *Scheduler) scheduleJob(job *Job) error {
	entryID, err := s.cron.AddFunc(job.Spec, s.createJobFunc(job))
	if err != nil {
		return err
	}

	s.mapMutex.Lock()
	s.jobMap[entryID] = job.ID
	s.mapMutex.Unlock()

	entry := s.cron.Entry(entryID)
	job.NextRun = entry.Next
	return s.storage.UpdateJob(context.Background(), job)
}

func (s *Scheduler) createJobFunc(job *Job) func() {
	return func() {
		s.semaphore <- struct{}{}
		defer func() { <-s.semaphore }()

		jobLock := s.getJobLock(job.ID)
		jobLock.Lock()
		defer jobLock.Unlock()

		ctx := context.Background()
		now := time.Now()
		job.Status = StatusRunning
		job.LastRun = &now
		s.storage.UpdateJob(ctx, job)

		start := time.Now()
		var lastError error

		defer func() {
			job.Latency = time.Since(start).Milliseconds()
			job.RunCount++

			if r := recover(); r != nil {
				lastError = errors.New("job panicked")
				job.Status = StatusFailed
				job.FailCount++
				job.LastError = lastError.Error()
			} else if lastError != nil {
				job.Status = StatusFailed
				job.FailCount++
				job.LastError = lastError.Error()
			} else {
				job.Status = StatusSuccess
				job.SuccessCount++
			}

			entryID := s.getEntryID(job.ID)
			if entryID != 0 {
				nextRun := s.cron.Entry(entryID).Next
				job.NextRun = nextRun
			}
			s.storage.UpdateJob(ctx, job)
		}()

		s.funcMu.RLock()
		fn, ok := s.funcMap[job.Payload.Func]
		s.funcMu.RUnlock()

		if !ok {
			lastError = ErrFunctionNotFound
			return
		}

		if s.config.JobTimeout > 0 {
			ctx, cancel := context.WithTimeout(ctx, s.config.JobTimeout)
			defer cancel()

			done := make(chan struct{})
			go func() {
				defer close(done)
				reflect.ValueOf(fn).Call(nil)
			}()

			select {
			case <-done:
			case <-ctx.Done():
				lastError = ErrJobTimeout
			}
		} else {
			reflect.ValueOf(fn).Call(nil)
		}
	}
}

func (s *Scheduler) removeJobFromCron(jobID uint) {
	entryID := s.getEntryID(jobID)
	if entryID != 0 {
		s.cron.Remove(entryID)
		s.mapMutex.Lock()
		delete(s.jobMap, entryID)
		s.mapMutex.Unlock()
	}
}

func (s *Scheduler) getEntryID(jobDBID uint) cron.EntryID {
	s.mapMutex.RLock()
	defer s.mapMutex.RUnlock()

	for id, dbID := range s.jobMap {
		if dbID == jobDBID {
			return id
		}
	}
	return 0
}

func (s *Scheduler) getJobLock(jobID uint) *sync.Mutex {
	s.jobLockMu.Lock()
	defer s.jobLockMu.Unlock()

	if lock, exists := s.jobLocks[jobID]; exists {
		return lock
	}
	lock := &sync.Mutex{}
	s.jobLocks[jobID] = lock
	return lock
}
