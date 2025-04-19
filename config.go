package jobscheduler

import "time"

// Config holds the configuration for the job scheduler
type Config struct {
	MaxConcurrentJobs int           // Maximum number of concurrent jobs
	JobTimeout        time.Duration // Timeout duration for job execution
	EnableWebUI       bool          // Whether to enable web interface
	WebListen         string        // Web interface listen address
}

// Default configuration values
const (
	DefaultMaxConcurrentJobs = 10
	DefaultJobTimeout        = 30 * time.Second
	DefaultWebListen         = "localhost:8080"
)

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		MaxConcurrentJobs: DefaultMaxConcurrentJobs,
		JobTimeout:        DefaultJobTimeout,
		EnableWebUI:       false,
		WebListen:         DefaultWebListen,
	}
}
