# Job Scheduler

[![Go Reference](https://pkg.go.dev/badge/github.com/Ctere1/jobscheduler.svg)](https://pkg.go.dev/github.com/Ctere1/jobscheduler)
[![Go Report Card](https://goreportcard.com/badge/github.com/Ctere1/jobscheduler)](https://goreportcard.com/report/github.com/Ctere1/jobscheduler)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A robust, PostgreSQL-backed job scheduler for Go applications with cron-style scheduling, concurrency control, and timeout handling.

## Features

- 🕒 **Cron-style scheduling** using standard cron expressions  
- 💾 **PostgreSQL persistence** for job durability across restarts  
- 🚦 **Concurrency control** with configurable job limits  
- ⏱️ **Timeout handling** for long-running jobs  
- 📊 **Job monitoring** with execution history and metrics  
- 🔄 **Automatic recovery** of scheduled jobs on restart  
- 🔒 **Thread-safe** implementation  

## Installation

```bash
go get github.com/Ctere1/jobscheduler
```

## Quick Start
```go
package main

import (
	"fmt"
	"time"

	"github.com/Ctere1/jobscheduler"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// Initialize database connection
	dsn := "host=localhost user=postgres password=postgres dbname=jobscheduler port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	// Create scheduler
	scheduler, err := jobscheduler.New(db, jobscheduler.Config{
		MaxConcurrentJobs: 5,
		JobTimeout:        30 * time.Second,
	})
	if err != nil {
		panic(err)
	}
	defer scheduler.Stop()

	// Define job function
	helloJob := func() {
		fmt.Println("Hello from scheduled job at", time.Now())
	}

	// Schedule job to run every minute
	job, err := scheduler.Schedule(
		"hello-job",
		"* * * * *",
		helloJob,
		nil,
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Scheduled job with ID: %s\n", job.JobID)

	// Keep program running
	select {}
}
```

## Documentation

For detailed documentation, including configuration options and advanced usage, please refer to the [GoDoc](https://pkg.go.dev/github.com/Ctere1/jobscheduler) page.

## Configuration

The scheduler can be configured using the `Config` struct. Here are the available options:
- `MaxConcurrentJobs`: Maximum number of jobs that can run concurrently. Default is 5.
- `JobTimeout`: Maximum time a job can run before being terminated. Default is 30 seconds.

```go
scheduler, err := jobscheduler.New(db, jobscheduler.Config{
    MaxConcurrentJobs: 10,
    JobTimeout:        1 * time.Minute,
})
```

## API Overview

### Core Methods
- `New(db *gorm.DB, config Config) (*Scheduler, error)`: Creates a new job scheduler instance.
- `Schedule(name, spec string, function any, metadata map[string]any)`: Schedules a new job.
- `Reschedule(identifier any, newSpec string, newMetadata map[string]any)`: Reschedules an existing job.
- `Unschedule(identifier any)`: Unschedules a job.
- `GetJob(identifier any)`: Retrieves a job by its identifier.
- `ListJobs() ([]Job, error)`: Lists all scheduled jobs.
- `RunNow(identifier any)`: Runs a job immediately.
- `Stop()`: Stops the scheduler.

### Job Model

- `JobID`: Unique identifier for the job.
- `Name`: Name of the job.
- `Spec`: Cron expression for scheduling.
- `Status`: Current status of the job (e.g., "scheduled", "running", "completed").
- `LastRun`: Timestamp of the last run.
- `NextRun`: Timestamp of the next scheduled run.
- `RunCount`: Number of times the job has run.
- `SuccessCount`: Number of successful runs.
- `FailureCount`: Number of failed runs.
- `Payload`: Additional metadata associated with the job.
- `Latency`: Time taken for the job to complete.


## Examples

### Scheduling different types of jobs

```go
// Daily report
scheduler.Schedule(
	"daily-report",
	"0 9 * * *", // 9:00 AM daily
	generateDailyReport,
	map[string]interface{}{
		"recipients": []string{"team@example.com"},
	},
)

// Frequent cleanup
scheduler.Schedule(
	"cleanup",
	"*/15 * * * *", // Every 15 minutes
	cleanupResources,
	nil,
)
    
```

### Monitoring jobs

```go
jobs, _ := scheduler.ListJobs()
for _, job := range jobs {
	if job.FailCount > 0 {
		fmt.Printf("Job %s failed %d times. Last error: %s\n",
			job.Name, job.FailCount, job.LastError)
	}
}
```

## Contributing

Contributions are welcome! Please read the [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to contribute to this project.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.




