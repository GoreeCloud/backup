package control

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

//go:embed static/*
var staticFS embed.FS

type Job struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CancelRequested bool `json:"cancel_requested,omitempty"`
	Output      string     `json:"output,omitempty"`
	Error       string     `json:"error,omitempty"`
	Source      string     `json:"-"`
	Destination string     `json:"-"`
	PreviewUsed bool       `json:"-"`
	ctx context.Context
	cancel context.CancelFunc
}

const maxPendingJobs = 12

type Server struct {
	token        string
	executor     Executor
	mux          *http.ServeMux
	mu           sync.Mutex
	jobs         []*Job
	pending      int
	queue        chan struct{}
	journal      *JobJournal
	journalError string
}

func ValidateAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return errors.New("provide a valid loopback host:port")
	}
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return errors.New("plaintext management API must bind to loopback only")
	}
	return nil
}

func NewServer(token string, executor Executor) http.Handler {
	return newServer(token, executor, nil, nil)
}

// NewServerWithState opts into a private on-disk history of job statuses only.
// Credentials, file paths, engine output and sync preview approvals stay in memory.
func NewServerWithState(token string, executor Executor, stateDir string) (http.Handler, error) {
	journal, previous, err := OpenJobJournal(stateDir)
	if err != nil {
		return nil, err
	}
	return newServer(token, executor, journal, previous), nil
}

func newServer(token string, executor Executor, journal *JobJournal, previous []*Job) http.Handler {
	if len(token) < 24 {
		panic("insufficient authentication token")
	}
	s := &Server{token: token, executor: executor, mux: http.NewServeMux(), queue: make(chan struct{}, 1), journal: journal, jobs: previous}
	s.mux.HandleFunc("GET /", s.index)
	s.mux.HandleFunc("GET /app.css", s.css)
	s.mux.HandleFunc("GET /app.js", s.js)
	s.mux.HandleFunc("GET /api/v1/health", s.auth(s.health))
	s.mux.HandleFunc("GET /api/v1/jobs", s.auth(s.listJobs))
	s.mux.HandleFunc("POST /api/v1/jobs", s.auth(s.createJob))
	s.mux.HandleFunc("GET /api/v1/jobs/{id}", s.auth(s.getJob))
	s.mux.HandleFunc("POST /api/v1/jobs/{id}/cancel", s.auth(s.cancelJob))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) static(w http.ResponseWriter, filename, mediaType string) {
	data, err := staticFS.ReadFile("static/" + filename)
	if err != nil {
		http.Error(w, "resource unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	_, _ = w.Write(data)
}
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.static(w, "index.html", "text/html; charset=utf-8")
}
func (s *Server) css(w http.ResponseWriter, r *http.Request) {
	s.static(w, "app.css", "text/css; charset=utf-8")
}
func (s *Server) js(w http.ResponseWriter, r *http.Request) {
	s.static(w, "app.js", "text/javascript; charset=utf-8")
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authorization, prefix) || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(authorization, prefix)), []byte(s.token)) != 1 {
			jsonError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		// No cross-origin write operations, even when a valid token is supplied.
		if r.Method != http.MethodGet {
			if origin := r.Header.Get("Origin"); origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Host != r.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					jsonError(w, http.StatusForbidden, "cross-origin requests are blocked")
					return
				}
			}
		}
		next(w, r)
	}
}

func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func jsonError(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	_, kopiaErr := exec.LookPath("kopia")
	_, rcloneErr := exec.LookPath("rclone")
	s.mu.Lock()
	persistent, journalError := s.journal != nil, s.journalError
	s.mu.Unlock()
	respond(w, 200, map[string]any{
		"service": "GoreeCloud Backup", "maturity": "development-alpha", "mode": "loopback-local",
		"engines":                map[string]bool{"kopia": kopiaErr == nil, "rclone": rcloneErr == nil},
		"job_history_persistent": persistent, "job_history_degraded": journalError != "",
	})
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := make([]Job, 0, len(s.jobs))
	for i := len(s.jobs) - 1; i >= 0; i-- {
		jobs = append(jobs, *s.jobs[i])
	}
	respond(w, 200, map[string]any{"jobs": jobs})
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	var found *Job
	for _, job := range s.jobs {
		if job.ID == r.PathValue("id") {
			copy := *job
			found = &copy
			break
		}
	}
	s.mu.Unlock()
	if found == nil {
		jsonError(w, 404, "job not found")
		return
	}
	respond(w, 200, found)
}

// cancelJob requests cancellation of a queued or running operation.
// Cancellation cannot undo storage changes already performed by an engine.
func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
    s.mu.Lock()
    var found *Job
    for _, job := range s.jobs {
        if job.ID == r.PathValue("id") { found = job; break }
    }
    if found == nil {
        s.mu.Unlock()
        jsonError(w, http.StatusNotFound, "job not found")
        return
    }
    switch found.Status {
    case "completed", "failed", "cancelled", "interrupted":
        copy := *found
        s.mu.Unlock()
        respond(w, http.StatusConflict, map[string]any{"error": "operation already finished", "job": copy})
        return
    }
    if !found.CancelRequested {
        found.CancelRequested = true
        found.Status = "cancelling"
        // A restart treats an unfinished cancellation as interrupted.
        if err := s.persistLocked(); err != nil { log.Printf("job metadata persistence failed: %v", err) }
        found.cancel()
    }
    copy := *found
    s.mu.Unlock()
    respond(w, http.StatusAccepted, copy)
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		jsonError(w, 415, "application/json is required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var requested Request
	if err := decoder.Decode(&requested); err != nil {
		jsonError(w, 400, "invalid request body")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		jsonError(w, 400, "only one JSON value is accepted")
		return
	}
	command, err := BuildCommand(requested)
	if err != nil {
		jsonError(w, 422, err.Error())
		return
	}
	// An unavailable engine is a configuration issue, not a silently successful job.
	if _, err := exec.LookPath(command.Binary); err != nil {
		jsonError(w, 503, command.Binary+" executable is not installed or not on PATH")
		return
	}
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		jsonError(w, 500, "cannot create job ID")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		ctx: ctx, cancel: cancel,
		ID: hex.EncodeToString(idBytes), Type: requested.Type, Status: "queued",
		StartedAt: time.Now().UTC(), Source: requested.Source, Destination: requested.Destination,
	}
	s.mu.Lock()
	if s.pending >= maxPendingJobs {
		cancel()
		s.mu.Unlock()
		jsonError(w, http.StatusTooManyRequests, "job queue full; retry after active jobs finish")
		return
	}
	if requested.Type == "sync" {
		// Require a successful, one-time preview for this exact source/destination.
		// A preview cannot guarantee unchanged data, but prevents blind deletion.
		matched := false
		for i := len(s.jobs) - 1; i >= 0; i-- {
			prior := s.jobs[i]
			if prior.Type == "sync-preview" && prior.Status == "completed" &&
				prior.Source == requested.Source && prior.Destination == requested.Destination &&
				prior.CompletedAt != nil && time.Since(*prior.CompletedAt) < 10*time.Minute && !prior.PreviewUsed {
				prior.PreviewUsed = true
				matched = true
				break
			}
		}
		if !matched {
			cancel()
			s.mu.Unlock()
			jsonError(w, http.StatusConflict, "run and review a successful sync-preview for these paths before confirming sync")
			return
		}
	}
	oldJobs := s.jobs
	s.pending++
	s.jobs = append(append([]*Job(nil), oldJobs...), job)
	if len(s.jobs) > 100 {
		s.jobs = s.jobs[1:]
	}
	if err := s.persistLocked(); err != nil {
		s.jobs = oldJobs
		s.pending--
		cancel()
		s.mu.Unlock()
		jsonError(w, 507, "could not persist job history")
		return
	}
	s.mu.Unlock()
	accepted := *job // response snapshot: the worker may update the live job concurrently
	go s.run(job, command)
	respond(w, 202, accepted)
}

func (s *Server) run(job *Job, command Command) {
    defer job.cancel()
    defer func(){ s.mu.Lock();s.pending--;s.mu.Unlock() }()
    // The timeout also includes time waiting for the single worker slot.
    ctx, timeoutCancel := context.WithTimeout(job.ctx, 30*time.Minute)
    defer timeoutCancel()
    select {
    case s.queue <- struct{}{}:
        defer func(){ <-s.queue }()
    case <-ctx.Done():
        s.finishJob(job, "", ctx.Err(), ctx.Err())
        return
    }
    s.mu.Lock()
    if ctx.Err() != nil {
        s.mu.Unlock()
        s.finishJob(job, "", ctx.Err(), ctx.Err())
        return
    }
    job.Status = "running"
    if err:=s.persistLocked();err!=nil{log.Printf("job metadata persistence failed: %v",err)}
    s.mu.Unlock()
    output,err:=s.executor.Run(ctx,command.Binary,command.Args...)
    s.finishJob(job,output,err,ctx.Err())
}

func (s *Server) finishJob(job *Job, output string, err error, reason error) {
    now:=time.Now().UTC()
    s.mu.Lock()
    defer s.mu.Unlock()
    job.CompletedAt=&now
    job.Output=strings.ReplaceAll(output,s.token,"[redacted]")
    switch {
    case err==nil:
        // A successful command may have completed immediately before cancel.
        job.Status="completed"
    case errors.Is(reason,context.Canceled):
        job.Status="cancelled"
        job.Error="operation cancelled; partial effects may require verification"
    case errors.Is(reason,context.DeadlineExceeded):
        job.Status="failed"
        job.Error="operation timed out; partial effects may require verification"
    default:
        job.Status="failed"
        job.Error="operation failed; inspect engine output"
    }
    if err:=s.persistLocked();err!=nil{log.Printf("job metadata persistence failed: %v",err)}
}

// Caller must hold s.mu. No file paths, output or secret environment values
// are serialized, including for failed jobs or snapshots.
func (s *Server) persistLocked() error {
	if s.journal == nil {
		return nil
	}
	err := s.journal.Persist(s.jobs)
	if err != nil {
		s.journalError = "job status could not be persisted"
	} else {
		s.journalError = ""
	}
	return err
}
