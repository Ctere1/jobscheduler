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
}

func New(db *gorm.DB, config Config) (*Scheduler, error) {
	if config.MaxConcurrentJobs <= 0 {
		config.MaxConcurrentJobs = 10
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
	}

	if err := r.restoreJobs(); err != nil {
		return nil, fmt.Errorf("failed to restore jobs: %w", err)
	}

	r.cron.Start()
	return r, nil
}

func (r *Scheduler) RegisterFunction(name string, fn any) {
	if reflect.TypeOf(fn).Kind() != reflect.Func {
		panic("only functions can be registered")
	}

	r.funcMu.Lock()
	defer r.funcMu.Unlock()
	r.funcMap[name] = fn
}

func (r *Scheduler) Schedule(name, spec string, fn any, metadata map[string]any) (*models.Job, error) {
	// Input validation
	if name == "" {
		return nil, ErrJobNameEmpty
	}
	if fn == nil {
		return nil, ErrJobFunctionNil
	}

	// Validate cron spec
	if _, err := cron.ParseStandard(spec); err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	// Get function info
	funcValue := reflect.ValueOf(fn)
	if funcValue.Kind() != reflect.Func {
		return nil, ErrJobFunctionInvalid
	}

	funcName := runtime.FuncForPC(funcValue.Pointer()).Name()
	if funcName == "" {
		return nil, ErrJobFunctionNameEmpty
	}

	// Register function
	r.RegisterFunction(funcName, fn)

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

func (r *Scheduler) Unschedule(jobID string) error {
	var job models.Job
	if err := r.db.Where("job_id = ?", jobID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrJobNotFound
		}
		return err
	}

	r.removeJobFromCron(job.ID)
	return r.db.Delete(&job).Error
}

func (r *Scheduler) ListJobs() ([]models.Job, error) {
	var jobs []models.Job
	err := r.db.Find(&jobs).Error
	return jobs, err
}

func (r *Scheduler) Stop() {
	r.cron.Stop()
}

// Private methods
func (r *Scheduler) restoreJobs() error {
	var jobs []models.Job
	if err := r.db.Find(&jobs).Error; err != nil {
		return err
	}

	for _, job := range jobs {
		if err := r.scheduleJob(&job); err != nil {
			continue
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
			done := make(chan struct{})
			go func() {
				defer close(done)
				reflect.ValueOf(fn).Call(nil)
			}()

			select {
			case <-done:
			case <-time.After(r.timeout):
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
