package jobscheduler

import (
	"embed"
	"html/template"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

//go:embed templates/*.gohtml
var templateFS embed.FS

type WebUI struct {
	scheduler *Scheduler
	echo      *echo.Echo
}

func NewWebUI(scheduler *Scheduler) *WebUI {
	e := echo.New()
	e.HideBanner = true

	tmpl := template.Must(template.ParseFS(templateFS, "templates/*.gohtml"))
	renderer := &echo.TemplateRenderer{
		Template: tmpl,
	}
	e.Renderer = renderer
	ui := &WebUI{
		scheduler: scheduler,
		echo:      e,
	}

	// API Routes
	api := e.Group("/api")
	{
		api.GET("/jobs", ui.listJobs)
		api.GET("/job-counts", ui.jobCounts)
		api.POST("/jobs/:id/run", ui.runJob)
		api.POST("/jobs/:id/unschedule", ui.unscheduleJob)
	}

	// Web UI Routes
	e.Static("/static", "static")
	e.GET("/", ui.dashboard)
	e.GET("/jobs/:id", ui.jobDetail)
	e.GET("/partials/jobs", ui.jobsPartial)

	return ui
}

func (ui *WebUI) Start(listen string) error {
	return ui.echo.Start(listen)
}

func (ui *WebUI) dashboard(c echo.Context) error {
	jobs, err := ui.scheduler.ListJobs()
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	counts := make(map[string]int)
	for _, job := range jobs {
		counts[string(job.Status)]++
	}

	data := map[string]any{
		"Jobs":      jobs,
		"Now":       time.Now(),
		"Config":    ui.scheduler.config,
		"JobCounts": counts,
	}

	return c.Render(http.StatusOK, "dashboard.gohtml", data)
}

func (ui *WebUI) jobsPartial(c echo.Context) error {
	jobs, err := ui.scheduler.ListJobs()
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	data := map[string]any{
		"Jobs": jobs,
	}
	return c.Render(http.StatusOK, "job_list.gohtml", data)
}

func (ui *WebUI) jobDetail(c echo.Context) error {
	jobID := c.Param("id")
	job, err := ui.scheduler.GetJob(jobID)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	data := map[string]any{
		"Job": job,
	}

	return c.Render(http.StatusOK, "job_detail.gohtml", data)
}

func (ui *WebUI) jobCounts(c echo.Context) error {
	jobs, err := ui.scheduler.ListJobs()
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	counts := make(map[string]int)
	for _, job := range jobs {
		counts[string(job.Status)]++
	}

	return c.JSON(http.StatusOK, counts)
}

func (ui *WebUI) listJobs(c echo.Context) error {
	jobs, err := ui.scheduler.ListJobs()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, jobs)
}

func (ui *WebUI) runJob(c echo.Context) error {
	jobID := c.Param("id")
	if err := ui.scheduler.RunNow(jobID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusOK)
}

func (ui *WebUI) unscheduleJob(c echo.Context) error {
	jobID := c.Param("id")
	if err := ui.scheduler.Unschedule(jobID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusOK)
}
