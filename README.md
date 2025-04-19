# Job Scheduler

[![Go Reference](https://pkg.go.dev/badge/github.com/Ctere1/jobscheduler.svg)](https://pkg.go.dev/github.com/Ctere1/jobscheduler)
[![Go Report Card](https://goreportcard.com/badge/github.com/Ctere1/jobscheduler)](https://goreportcard.com/report/github.com/Ctere1/jobscheduler)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight, persistent, and concurrency-aware job scheduler for Go.  
Designed to register, schedule, execute, and manage cron-based jobs with metadata, optional web UI, and persistent storage.

> ✅ Built with `gorm`, `robfig/cron/v3`, and designed to be integrated into any Go application as a library.

## ✨ Features

- ⏰ Cron-based job scheduling  
- 💾 Persistent storage (via GORM)  
- 🔁 Automatic job restoration on restart  
- 🔒 Concurrency control via semaphore  
- 🧠 Reflection-based function registration  
- 🧩 Idempotent job definitions (safe to call on each boot)  
- 📈 Job execution tracking (status, latency, failure count)  
- 🖥️ Optional web-based UI for monitoring (toggle via config)  

## 📦 Installation

```bash
go get github.com/Ctere1/jobscheduler
```

## ⚙️ Initialization Example

```go
import (
	"time"
	"log"

	"github.com/Ctere1/jobscheduler"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	dsn := "host=localhost user=postgres password=postgres dbname=jobscheduler port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	scheduler, err := jobscheduler.New(db, jobscheduler.Config{
		MaxConcurrentJobs: 5,
		JobTimeout:        30 * time.Second,
		EnableWebUI:       true,
		WebListen:         "127.0.0.1:8080",
	})
	if err != nil {
		log.Fatal(err)
	}

	scheduler.Schedule("daily-cleanup", "0 3 * * *", cleanupTask, nil)

	select {}
}

func cleanupTask() {
	log.Println("Running cleanup...")
}
```

## 🧠 How It Works

### 1. Job Registration

Jobs are registered with a name, cron spec, function, and optional metadata.  
The function is registered using reflection and stored by name.

### 2. Persistence

All jobs are persisted to the database (`gorm`-compatible) using the `Job` model.  
On startup, jobs are restored and scheduled automatically.

### 3. Concurrency Control

Job concurrency is managed using a buffered channel (`chan struct{}`) as a **semaphore**.  
This allows you to set the maximum number of parallel jobs via `MaxConcurrentJobs`.

### 4. Timeouts

Jobs can have a maximum execution time.  
If they exceed `JobTimeout`, they are marked as failed.

### 5. Rescheduling

Calling `Schedule()` on a job with the same name updates the cron spec/metadata if it already exists — making boot-time job declarations **safe and idempotent**.


## 🔧 Configuration

The scheduler can be configured using the `Config` struct. Here are the available options:
- `MaxConcurrentJobs`: Maximum number of jobs that can run concurrently. Default is 5.
- `JobTimeout`: Maximum time a job can run before being terminated. Default is 30 seconds.
- `EnableWebUI`: Enable or disable the built-in web UI. Default is false.
- `WebListen`: Address for the web UI to listen on. Default is "127.0.0.1:8080".

```go
scheduler, err := jobscheduler.New(db, jobscheduler.Config{
    MaxConcurrentJobs: 5,
    JobTimeout:        30 * time.Second,
	EnableWebUI:       false,
	WebListen:         "127.0.0.1:8080"
})
```

## 🛠️ API Overview

| Method       | Description                                  |
|--------------|----------------------------------------------|
| Schedule     | Register a new job (or reschedule if exists) |
| Reschedule   | Update the schedule and metadata             |
| Unschedule   | Remove the job from DB and cron              |
| RunNow       | Run job immediately                          |
| ListJobs     | Retrieve all jobs                            |
| GetJob       | Lookup job by ID or name                     |
| Stop         | Stop the scheduler                           |


## 🧩 Example: Multiple Jobs

```go
scheduler.Schedule("hello", "*/5 * * * *", func() {
	fmt.Println("Hello from job!")
}, nil)

scheduler.Schedule("system-metrics", "0 */1 * * *", collectMetrics, map[string]any{
	"source": "agent",
})

jobs, _ := scheduler.ListJobs()
for _, job := range jobs {
	if job.FailCount > 0 {
		fmt.Printf("Job %s failed %d times. Last error: %s\n", 
		job.Name, job.FailCount, job.LastError)
	}
}
```

## 📘 Job Model Schema

Internally, jobs are stored with the following fields:

- `JobID`: UUID  
- `Name`: Unique job name  
- `Spec`: Cron expression  
- `Payload.Func`: Registered function name (via reflection)  
- `Payload.Data`: Metadata map  
- `Status`: Scheduled / Running / Success / Failed  
- `RunCount`: Total runs  
- `SuccessCount`: Successful runs  
- `FailCount`: Failed runs  
- `LastError`: Error from last failure (if any)  
- `LastRun`: Timestamp of last run  
- `NextRun`: Timestamp of next run (auto-calculated)  
- `Latency`: Duration of last execution in milliseconds  


## 🖥️ Optional Web UI

If `EnableWebUI` is set to `true`, a basic web dashboard is served.

```go
Config{
	EnableWebUI: true,
	WebListen:   "127.0.0.1:8080",
}
```

This dashboard provides:
- Job list and status  
- Next/last run timestamps  
- Run/fail counts  

## Documentation

For detailed documentation, including configuration options and advanced usage, please refer to the [GoDoc](https://pkg.go.dev/github.com/Ctere1/jobscheduler) page.

## Contributing

Contributions are welcome! Please read the [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to contribute to this project.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.