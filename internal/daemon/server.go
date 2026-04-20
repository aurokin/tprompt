package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultHandlerDeadline bounds how long a single request/response cycle on
// the server side can take. Same rationale as the client deadline — protect
// against a stuck peer.
const DefaultHandlerDeadline = 5 * time.Second

// StaleSocketProbeTimeout is how long Listen waits when probing a leftover
// socket file to decide whether another daemon owns it.
const StaleSocketProbeTimeout = 200 * time.Millisecond

// StatusFunc returns a fresh StatusResponse snapshot. The Server calls this
// per status request so callers control which fields are reported (pid,
// uptime, etc.) without the server depending on a clock or process info.
type StatusFunc func() StatusResponse

// ServerConfig wires a Server with the queue it submits to and the metadata
// emitter for status requests.
type ServerConfig struct {
	SocketPath string
	Queue      *Queue
	Logger     *Logger
	StatusFn   StatusFunc
}

// Server accepts JSON-framed daemon requests on a Unix domain socket.
// Lifecycle: NewServer → Listen → Serve (blocks) → Close.
type Server struct {
	socketPath string
	queue      *Queue
	logger     *Logger
	statusFn   StatusFunc

	jobCounter atomic.Uint64
	now        func() time.Time
	dial       func(network, address string, timeout time.Duration) (net.Conn, error)

	listener  net.Listener
	handlerWG sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

// NewServer constructs a server with the given dependencies. Listen must be
// called before Serve. Panics if Queue, Logger, or StatusFn is nil — those
// are programmer errors at wiring time, not runtime conditions.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Queue == nil {
		panic("daemon: ServerConfig.Queue is nil")
	}
	if cfg.Logger == nil {
		panic("daemon: ServerConfig.Logger is nil")
	}
	if cfg.StatusFn == nil {
		panic("daemon: ServerConfig.StatusFn is nil")
	}
	return &Server{
		socketPath: cfg.SocketPath,
		queue:      cfg.Queue,
		logger:     cfg.Logger,
		statusFn:   cfg.StatusFn,
		now:        time.Now,
		dial:       net.DialTimeout,
	}
}

// nextJobID mints a daemon-owned, monotonic-within-process job ID.
// Format: j-<unix-nanos>-<atomic-counter>. The unix-nanos prefix sorts
// chronologically across daemon restarts; the counter guarantees uniqueness
// when two submits share the same nanosecond.
func (s *Server) nextJobID() string {
	return fmt.Sprintf("j-%d-%d", s.now().UnixNano(), s.jobCounter.Add(1))
}

// Listen binds the Unix socket. Mkdir-p's the parent directory at 0700 and
// chmod's the socket to 0600. If a stale socket file exists at the path,
// Listen probes it: a successful dial means another daemon already owns
// this socket and Listen returns SocketUnavailableError; only a definitive
// "no listener" dial failure is treated as stale and unlinked before bind.
func (s *Server) Listen() error {
	if s.socketPath == "" {
		return &SocketUnavailableError{Reason: "socket path is empty"}
	}
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o700); err != nil {
		return &SocketUnavailableError{Path: s.socketPath, Reason: "create dir: " + err.Error()}
	}
	if err := s.cleanupStaleSocket(); err != nil {
		return err
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return &SocketUnavailableError{Path: s.socketPath, Reason: err.Error()}
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(s.socketPath)
		return &SocketUnavailableError{Path: s.socketPath, Reason: "chmod: " + err.Error()}
	}
	s.listener = listener
	return nil
}

// SocketPath returns the bound socket path. Useful for status reporting and
// tests after Listen.
func (s *Server) SocketPath() string { return s.socketPath }

// Serve accepts connections until the listener is closed. Returns nil on
// graceful shutdown (Close invoked) or the underlying error otherwise.
// Safe to run in its own goroutine; the listener reference is captured at
// entry so concurrent Close calls don't race.
func (s *Server) Serve() error {
	listener := s.listener
	if listener == nil {
		return errors.New("daemon: Serve called before Listen")
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.handlerWG.Add(1)
		go s.handle(conn)
	}
}

// Close stops accepting new connections and waits for in-flight handlers to
// finish. The Unix listener unlinks the socket file on Close (per stdlib).
// Idempotent and safe to call concurrently.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		if s.listener == nil {
			return
		}
		s.closeErr = s.listener.Close()
		s.handlerWG.Wait()
	})
	return s.closeErr
}

// RunExitReason describes why Run returned after the daemon successfully
// started listening.
type RunExitReason string

const (
	// RunExitContextCanceled reports orderly shutdown initiated by the caller's
	// context (for example SIGINT/SIGTERM in the CLI wrapper).
	RunExitContextCanceled RunExitReason = "context_canceled"
	// RunExitServeError reports that the accept loop exited unexpectedly.
	RunExitServeError RunExitReason = "serve_error"
)

// RunResult summarizes the daemon lifecycle after Run returns.
type RunResult struct {
	Started    bool
	ExitReason RunExitReason
}

func (s *Server) cleanupStaleSocket() error {
	info, err := os.Stat(s.socketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return &SocketUnavailableError{Path: s.socketPath, Reason: "stat: " + err.Error()}
	}
	if info.Mode()&os.ModeSocket == 0 {
		return &SocketUnavailableError{
			Path:   s.socketPath,
			Reason: "path exists and is not a socket",
		}
	}
	conn, err := s.dial("unix", s.socketPath, StaleSocketProbeTimeout)
	if err == nil {
		_ = conn.Close()
		return &SocketUnavailableError{Path: s.socketPath, Reason: "already running"}
	}
	if !isDefinitivelyStaleSocketProbeError(err) {
		return &SocketUnavailableError{Path: s.socketPath, Reason: dialReason(err)}
	}
	if rmErr := os.Remove(s.socketPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		return &SocketUnavailableError{
			Path:   s.socketPath,
			Reason: fmt.Sprintf("remove stale socket: %v", rmErr),
		}
	}
	return nil
}

func isDefinitivelyStaleSocketProbeError(err error) bool {
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return true
	case errors.Is(err, syscall.ENOENT), errors.Is(err, os.ErrNotExist):
		return true
	default:
		return false
	}
}

func (s *Server) handle(conn net.Conn) {
	defer s.handlerWG.Done()
	defer func() { _ = conn.Close() }()

	_ = conn.SetDeadline(time.Now().Add(DefaultHandlerDeadline))

	var req wireRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = s.logger.Log(Entry{Outcome: OutcomeIPCError, Msg: "decode request: " + err.Error()})
		s.writeError(conn, "bad request: "+err.Error())
		return
	}

	switch req.Kind {
	case kindSubmit:
		if req.Submit == nil {
			s.writeError(conn, "submit request missing payload")
			return
		}
		if err := validateJob(req.Submit.Job); err != nil {
			_ = s.logger.Log(Entry{
				Source:  req.Submit.Job.Source,
				Outcome: OutcomeIPCError,
				Msg:     "reject submit: " + err.Error(),
			})
			s.writeError(conn, err.Error())
			return
		}
		// Daemon owns the canonical job ID; any client-supplied value is
		// discarded so logs and the response always reference the same key.
		req.Submit.Job.JobID = s.nextJobID()
		resp := s.queue.Enqueue(req.Submit.Job)
		s.writeResponse(conn, wireResponse{Kind: kindSubmit, Submit: &resp})
	case kindStatus:
		status := s.statusFn()
		s.writeResponse(conn, wireResponse{Kind: kindStatus, Status: &status})
	default:
		s.writeError(conn, fmt.Sprintf("unknown request kind: %q", req.Kind))
	}
}

func (s *Server) writeResponse(conn net.Conn, resp wireResponse) {
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		_ = s.logger.Log(Entry{Outcome: OutcomeIPCError, Msg: "write response: " + err.Error()})
	}
}

func (s *Server) writeError(conn net.Conn, msg string) {
	s.writeResponse(conn, wireResponse{Error: msg})
}

// Run binds the socket, starts serving, and blocks until ctx is cancelled.
// On shutdown it always closes the listener before canceling workers so no new
// jobs can race with teardown. Signal-driven shutdown then cancels accepted
// work and waits for those workers to observe cancellation before returning.
// If Serve exits unexpectedly, Run follows the same worker-cancellation path
// so failure shutdown is equally prompt. This is the entrypoint the `tprompt
// daemon start` CLI shim calls; tests can wire their own context for
// deterministic shutdown.
//
// onReady, if non-nil, fires exactly once after Listen has succeeded and
// before Serve begins accepting. It is the only point at which the caller
// can know the server is live — Listen failures return before onReady is
// invoked, so callers can gate "daemon started" banners and logs on whether
// it fired.
func Run(ctx context.Context, s *Server, onReady func()) (RunResult, error) {
	result := RunResult{}
	if err := s.Listen(); err != nil {
		return result, err
	}
	result.Started = true
	if onReady != nil {
		onReady()
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve() }()

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		result.ExitReason = RunExitServeError
		closeErr := s.Close()
		s.queue.CancelAll()
		s.queue.Wait()
		return result, errors.Join(err, closeErr)
	}

	result.ExitReason = RunExitContextCanceled
	closeErr := s.Close()
	s.queue.CancelAll()
	s.queue.Wait()
	// Drain the serve goroutine so we don't return before it exits.
	serveCloseErr := <-serveErr
	return result, errors.Join(closeErr, serveCloseErr)
}
