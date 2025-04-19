package jobscheduler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/Ctere1/jobscheduler/models"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type Config struct {
	MaxConcurrentJobs int
	JobTimeout        time.Duration
}

type Scheduler struct {
	db         *gorm.DB
	cron       *cron.Cron
	jobMap     map[cron.EntryID]uint // entryID -> job DB ID
	mapMutex   sync.RWMutex
	jobChannel chan struct{}
	timeout    time.Duration
	funcMap    map[string]any
	funcMu     sync.RWMutex
	jobLocks   map[uint]*sync.Mutex
	jobLockMu  sync.Mutex
}

func New(db *gorm.DB, config Config) (*Scheduler, error) {
	if config.MaxConcurrentJobs <= 0 {
		config.MaxConcurrentJobs = 10
	}
	if config.JobTimeout <= 0 {
		config.JobTimeout = 30 * time.Second
	}

	if err := db.AutoMigrate(&models.Job{}); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	r := &Scheduler{
		db:         db,
		cron:       cron.New(cron.WithChain(cron.Recover(cron.DefaultLogger))),
		jobMap:     make(map[cron.EntryID]uint),
		jobChannel: make(chan struct{}, config.MaxConcurrentJobs),
		timeout:    config.JobTimeout,
		funcMap:    make(map[string]any),
		jobLocks:   make(map[uint]*sync.Mutex),
	}

	if err := r.restoreJobs(); err != nil {
		return nil, fmt.Errorf("failed to restore jobs: %w", err)
	}

	r.cron.Start()
	return r, nil
}

func (r *Scheduler) Schedule(name, spec string, function any, metadata map[string]any) (*models.Job, error) {
	// Input validation
	if name == "" {
		return nil, ErrJobNameEmpty
	}
	if function == nil {
		return nil, ErrJobFunctionNil
	}

	// Validate cron spec
	if _, err := cron.ParseStandard(spec); err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
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
	r.registerFunction(funcName, function)

	// Check if job with same name already exists
	existingJob, err := r.getJobByIdentifier(name)
	if err == nil {
		oldFuncName := existingJob.Payload.Func
		if oldFuncName != funcName {
			// Reschedule the job with new function
			updatedJob, err := r.Reschedule(existingJob.JobID, spec, metadata)
			if err == nil {
				r.funcMu.Lock()
				delete(r.funcMap, oldFuncName)
				r.funcMu.Unlock()
			}
			return updatedJob, err
		}
		return r.Reschedule(existingJob.JobID, spec, metadata)
	}

	// Create job object
	job := &models.Job{
		JobID: uuid.New().String(),
		Name:  name,
		Spec:  spec,
		Payload: models.JobPayload{
			Type: models.TypeFunction,
			Name: name,
			Func: funcName,
			Data: metadata,
		},
		Status: models.StatusScheduled,
	}

	if err := r.db.Create(job).Error; err != nil {
		return nil, err
	}

	if err := r.scheduleJob(job); err != nil {
		r.db.Delete(job)
		return nil, err
	}

	return job, nil
}

// Reschedule updates an existing job with new parameters
func (r *Scheduler) Reschedule(identifier any, newSpec string, newMetadata map[string]any) (*models.Job, error) {
	job, err := r.getJobByIdentifier(identifier)
	if err != nil {
		return nil, err
	}

	// Validate new cron spec if it's being changed
	if newSpec != "" {
		if _, err := cron.ParseStandard(newSpec); err != nil {
			return nil, fmt.Errorf("invalid cron expression: %w", err)
		}
		job.Spec = newSpec
	}

	// Update metadata if provided
	if newMetadata != nil {
		job.Payload.Data = newMetadata
	}

	// Remove existing schedule
	r.removeJobFromCron(job.ID)

	// Add new schedule
	if err := r.scheduleJob(job); err != nil {
		return nil, fmt.Errorf("failed to reschedule job: %w", err)
	}

	// Save updated job
	if err := r.db.Save(job).Error; err != nil {
		return nil, fmt.Errorf("failed to save job updates: %w", err)
	}

	return job, nil
}

// Unschedule by either job ID or name
func (r *Scheduler) Unschedule(identifier any) error {
	job, err := r.getJobByIdentifier(identifier)
	if err != nil {
		return err
	}

	r.removeJobFromCron(job.ID)

	r.funcMu.Lock()
	delete(r.funcMap, job.Payload.Func)
	r.funcMu.Unlock()

	return r.db.Delete(job).Error
}

// RunNow executes a job immediately
func (r *Scheduler) RunNow(identifier any) error {
	job, err := r.getJobByIdentifier(identifier)
	if err != nil {
		return err
	}

	jobLock := r.getJobLock(job.ID)
	jobLock.Lock()
	defer jobLock.Unlock()

	if job.Status == models.StatusRunning {
		return fmt.Errorf("job %s is already running", job.Name)
	}

	select {
	case r.jobChannel <- struct{}{}:
		go func() {
			defer func() { <-r.jobChannel }()
			r.createJobFunc(job)()
		}()
		return nil
	default:
		return fmt.Errorf("concurrency limit reached, try again later")
	}
}

// GetJob returns a job by either ID or name
func (r *Scheduler) GetJob(identifier any) (*models.Job, error) {
	return r.getJobByIdentifier(identifier)
}

// ListJobs returns all jobs
func (r *Scheduler) ListJobs() ([]models.Job, error) {
	var jobs []models.Job
	err := r.db.Find(&jobs).Error
	return jobs, err
}

func (r *Scheduler) Stop() {
	r.cron.Stop()
}

// Private methods

// getJobByIdentifier helper function to get job by either ID or name
func (r *Scheduler) getJobByIdentifier(identifier any) (*models.Job, error) {
	var job models.Job
	var query *gorm.DB

	switch v := identifier.(type) {
	case string:
		// Try to parse as UUID first (job ID)
		if _, err := uuid.Parse(v); err == nil {
			query = r.db.Where("job_id = ?", v)
		} else {
			query = r.db.Where("name = ?", v)
		}
	case uuid.UUID:
		query = r.db.Where("job_id = ?", v.String())
	default:
		return nil, fmt.Errorf("invalid identifier type: %T", identifier)
	}

	if err := query.First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}

	return &job, nil
}

func (r *Scheduler) registerFunction(name string, fn any) {
	if reflect.TypeOf(fn).Kind() != reflect.Func {
		panic("only functions can be registered")
	}

	r.funcMu.Lock()
	defer r.funcMu.Unlock()
	r.funcMap[name] = fn
}

func (r *Scheduler) restoreJobs() error {
	var jobs []models.Job
	if err := r.db.Order("created_at DESC").Find(&jobs).Error; err != nil {
		return err
	}

	seen := make(map[string]bool)
	for _, job := range jobs {
		if seen[job.Name] {
			continue // Skip if job with same name already exists
		}
		seen[job.Name] = true

		if err := r.scheduleJob(&job); err != nil {
			// Log error but continue
			fmt.Printf("Failed to restore job %s: %v\n", job.Name, err)
		}
	}
	return nil
}

func (r *Scheduler) scheduleJob(job *models.Job) error {
	entryID, err := r.cron.AddFunc(job.Spec, r.createJobFunc(job))
	if err != nil {
		return err
	}

	r.mapMutex.Lock()
	r.jobMap[entryID] = job.ID
	r.mapMutex.Unlock()

	entry := r.cron.Entry(entryID)
	job.NextRun = entry.Next
	return r.db.Save(job).Error
}

func (r *Scheduler) createJobFunc(job *models.Job) func() {
	return func() {
		r.jobChannel <- struct{}{}
		defer func() { <-r.jobChannel }()

		jobLock := r.getJobLock(job.ID)
		jobLock.Lock()
		defer jobLock.Unlock()

		ctx := context.Background()
		now := time.Now()
		job.Status = models.StatusRunning
		job.LastRun = &now
		r.db.WithContext(ctx).Save(job)

		start := time.Now()
		var lastError error

		defer func() {
			job.Latency = time.Since(start).Milliseconds()
			job.RunCount++

			if r := recover(); r != nil {
				lastError = fmt.Errorf("job panicked: %v", r)
				job.Status = models.StatusFailed
				job.FailCount++
				job.LastError = lastError.Error()
			} else if lastError != nil {
				job.Status = models.StatusFailed
				job.FailCount++
				job.LastError = lastError.Error()
			} else {
				job.Status = models.StatusSuccess
				job.SuccessCount++
			}

			entryID := r.getEntryID(job.ID)
			if entryID != 0 {
				job.NextRun = r.cron.Entry(entryID).Next
			}
			r.db.WithContext(ctx).Save(job)
		}()

		r.funcMu.RLock()
		fn, ok := r.funcMap[job.Payload.Func]
		r.funcMu.RUnlock()

		if !ok {
			lastError = ErrFunctionNotFound
			return
		}

		if r.timeout > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
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

func (r *Scheduler) removeJobFromCron(jobDBID uint) {
	entryID := r.getEntryID(jobDBID)
	if entryID != 0 {
		r.cron.Remove(entryID)
		r.mapMutex.Lock()
		delete(r.jobMap, entryID)
		r.mapMutex.Unlock()
	}
}

func (r *Scheduler) getEntryID(jobDBID uint) cron.EntryID {
	r.mapMutex.RLock()
	defer r.mapMutex.RUnlock()

	for id, dbID := range r.jobMap {
		if dbID == jobDBID {
			return id
		}
	}
	return 0
}

func (r *Scheduler) getJobLock(jobID uint) *sync.Mutex {
	r.jobLockMu.Lock()
	defer r.jobLockMu.Unlock()

	if r.jobLocks == nil {
		r.jobLocks = make(map[uint]*sync.Mutex)
	}
	if _, ok := r.jobLocks[jobID]; !ok {
		r.jobLocks[jobID] = &sync.Mutex{}
	}
	return r.jobLocks[jobID]
}
