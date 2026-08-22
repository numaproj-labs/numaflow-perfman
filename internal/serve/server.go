package serve

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
	"numa-perfman/internal/results"
)

const (
	defaultBenchmark   = "single-map"
	plotlyStaticPath   = "/static/plotly.min.js"
	plotlyScriptForWeb = plotlyStaticPath
)

// Options configures the serve HTTP server.
type Options struct {
	Repo    *results.Repository
	Config  config.Config
	Client  cluster.Client
	Version string
	// EnableJobs starts the in-process benchmark JobManager.
	EnableJobs bool
}

// Server serves the benchmark report web UI and APIs.
type Server struct {
	repo    *results.Repository
	cfg     config.Config
	client  cluster.Client
	version string
	jobs    *JobManager
	static  http.Handler
}

// New constructs a read-only Server backed by the results repository.
func New(repo *results.Repository) *Server {
	return NewWithOptions(Options{Repo: repo})
}

// NewWithOptions constructs a Server from Options.
func NewWithOptions(opts Options) *Server {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Sprintf("serve static fs: %v", err))
	}
	s := &Server{
		repo:    opts.Repo,
		cfg:     opts.Config,
		client:  opts.Client,
		version: opts.Version,
		static:  http.StripPrefix("/static/", http.FileServer(http.FS(sub))),
	}
	if opts.EnableJobs && opts.Repo != nil {
		s.jobs = NewJobManager(opts.Repo, opts.Config, opts.Client, opts.Version)
	}
	return s
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/" || path == "/index.html":
		s.serveIndex(w, r)
	case strings.HasPrefix(path, "/static/"):
		s.static.ServeHTTP(w, r)
	case path == "/api/benchmarks":
		s.handleBenchmarks(w, r)
	case path == "/api/runs":
		s.handleRuns(w, r)
	case path == "/api/report":
		s.handleReport(w, r)
	case path == "/api/compare":
		s.handleCompare(w, r)
	case path == "/api/benchmark-runs":
		s.handleBenchmarkRuns(w, r)
	case strings.HasPrefix(path, "/api/benchmark-runs/"):
		s.handleBenchmarkRunSubpath(w, r)
	case path == "/api/validations":
		s.handleValidations(w, r)
	case path == "/api/validation-runs":
		s.handleValidationRuns(w, r)
	case strings.HasPrefix(path, "/api/validation-runs/"):
		s.handleValidationRunSubpath(w, r)
	case path == "/api/preflight":
		s.handlePreflight(w, r)
	case path == "/api/config":
		s.handleConfig(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// Run starts the HTTP server until ctx is cancelled.
func Run(ctx context.Context, addr string, repo *results.Repository) error {
	return RunWithOptions(ctx, addr, Options{Repo: repo})
}

// RunWithOptions starts the HTTP server with full Options until ctx is cancelled.
func RunWithOptions(ctx context.Context, addr string, opts Options) error {
	s := NewWithOptions(opts)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		if s.jobs != nil {
			s.jobs.Shutdown(2 * time.Minute)
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		err := <-errCh
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
