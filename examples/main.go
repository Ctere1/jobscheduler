package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ctere1/jobscheduler"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// 1. Create a database connection
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
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 2. Scheduler configuration
	config := jobscheduler.Config{
		MaxConcurrentJobs: 3,
		JobTimeout:        30 * time.Second,
		EnableWebUI:       true,
		WebListen:         "localhost:8080",
	}

	// 3. Start the scheduler
	scheduler, err := jobscheduler.New(db, config)
	if err != nil {
		log.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Stop()

	// 4. Define example job functions
	helloJob := func() {
		fmt.Printf("[HelloJob] Executed at: %s\n", time.Now().Format(time.RFC3339))
		time.Sleep(1 * time.Second) // Simulated workload
	}

	reportJob := func() {
		fmt.Printf("[ReportJob] Generating report...\n")
		time.Sleep(2 * time.Second)
		fmt.Printf("[ReportJob] Report completed\n")
	}

	// 5. Schedule the jobs (every 10 seconds and every 20 seconds for quick testing)
	_, err = scheduler.Schedule("hello-job", "@every 10s", helloJob, map[string]any{
		"creator": "admin",
		"tags":    []string{"greeting", "test"},
	})
	if err != nil {
		log.Printf("Failed to schedule hello-job: %v", err)
	}

	reportJobID, err := scheduler.Schedule("daily-report", "@every 20s", reportJob, nil)
	if err != nil {
		log.Printf("Failed to schedule daily-report: %v", err)
	} else {
		fmt.Printf("Daily report job scheduled with ID: %s\n", reportJobID.JobID)
	}

	// 6. Example: Run job manually
	go func() {
		time.Sleep(5 * time.Second)
		fmt.Println("\nManually running hello-job...")
		if err := scheduler.RunNow("hello-job"); err != nil {
			log.Printf("RunNow failed: %v", err)
		}
	}()

	// 7. List current jobs
	jobs, err := scheduler.ListJobs()
	if err != nil {
		log.Printf("Failed to list jobs: %v", err)
	} else {
		fmt.Println("\nCurrent Jobs:")
		for _, job := range jobs {
			fmt.Printf("- %s (ID: %s) Next Run: %v\n", job.Name, job.JobID, job.NextRun)
		}
	}

	// 8. Example: Reschedule a job
	go func() {
		time.Sleep(10 * time.Second)
		fmt.Println("\nUpdating the daily report job schedule...")
		updatedJob, err := scheduler.Reschedule("daily-report", "@every 30s", map[string]any{
			"recipients": []string{"manager@example.com", "team@example.com"},
		})
		if err != nil {
			log.Printf("Reschedule failed: %v", err)
		} else {
			fmt.Printf("Job rescheduled. New schedule: %s\n", updatedJob.Spec)
		}
	}()

	// 9. Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down application...")
}
