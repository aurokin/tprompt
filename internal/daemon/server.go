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

	"github.com/hsadler/tprompt/internal/daemon/lifecycle"
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

// IdentityFunc returns the lifecycle.Identity to write next to the socket
// after Listen acquires the run lock. Callers know pid/exec/version; the
// daemon package stays free of CLI-version knowledge. When IdentityFunc is
// nil, no identity sidecar is written and the corresponding remove on
// Close is skipped.
type IdentityFunc func() lifecycle.Identity

// ServerConfig wires a Server with the queue it submits to and the metadata
// emitter for status requests.
type ServerConfig struct {
	SocketPath string
	Queue      *Queue
	Logger     *Logger
	StatusFn   StatusFunc
	ShutdownFn func()
	// IdentityFn supplies the per-daemon ownership record written to
	// <socket>.identity.json after the run lock is acquired. Optional in
	// tests; production callers populate it.
	IdentityFn IdentityFunc
}

// Server accepts JSON-framed daemon requests on a Unix domain socket.
// Lifecycle: NewServer → Listen → Serve (blocks) → Close.
type Server struct {
	socketPath string
	queue      *Queue
	logger     *Logger
	statusFn   StatusFunc
	shutdownFn func()
	identityFn IdentityFunc

	jobCounter atomic.Uint64
	now        func() time.Time
	dial       func(network, address string, timeout time.Duration) (net.Conn, error)

	// Lifecycle-ownership state captured during Listen.
	paths        lifecycle.Paths
	runLock      *lifecycle.RunLock
	identity     lifecycle.Identity
	identityWrit bool

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
		shutdownFn: cfg.ShutdownFn,
		identityFn: cfg.IdentityFn,
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

// Listen binds the Unix socket. Sequence:
//
//  1. Acquire the lifecycle run lock (`<socket>.lock`) before any other
//     filesystem state mutation. Two daemons cannot pass this point
//     simultaneously.
//  2. Run conservative stale-socket cleanup, which now consults the run
//     lock and identity sidecar to decide whether a leftover socket is
//     safe to unlink.
//  3. Bind the unix socket and chmod it 0600.
//  4. Write the identity sidecar so other processes can identify the
//     owner.
//
// If any step after run-lock acquisition fails, the run lock is released
// before returning so a retry can proceed.
func (s *Server) Listen() error {
	if s.socketPath == "" {
		return &SocketUnavailableError{Reason: "socket path is empty"}
	}
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o700); err != nil {
		return &SocketUnavailableError{Path: s.socketPath, Reason: "create dir: " + err.Error()}
	}
	paths, err := lifecycle.PathsFor(s.socketPath)
	if err != nil {
		return &SocketUnavailableError{Path: s.socketPath, Reason: "lifecycle paths: " + err.Error()}
	}
	s.paths = paths

	runLock, err := lifecycle.AcquireRunLock(paths)
	if err != nil {
		if errors.Is(err, lifecycle.ErrLockHeld) {
			return &SocketUnavailableError{Path: s.socketPath, Reason: "run lock held by another daemon"}
		}
		return &SocketUnavailableError{Path: s.socketPath, Reason: "acquire run lock: " + err.Error()}
	}
	s.runLock = runLock

	if err := s.cleanupStaleSocket(); err != nil {
		_ = s.runLock.Release()
		s.runLock = nil
		return err
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		_ = s.runLock.Release()
		s.runLock = nil
		return &SocketUnavailableError{Path: s.socketPath, Reason: err.Error()}
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(s.socketPath)
		_ = s.runLock.Release()
		s.runLock = nil
		return &SocketUnavailableError{Path: s.socketPath, Reason: "chmod: " + err.Error()}
	}
	s.listener = listener

	if s.identityFn != nil {
		s.identity = s.identityFn()
		if err := lifecycle.WriteIdentity(paths, s.identity); err != nil {
			_ = listener.Close()
			_ = os.Remove(s.socketPath)
			_ = s.runLock.Release()
			s.runLock = nil
			return &SocketUnavailableError{Path: s.socketPath, Reason: "write identity: " + err.Error()}
		}
		s.identityWrit = true
	}
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
// After the listener is closed the lifecycle ownership artifacts are torn
// down: identity sidecar removed only when it still matches our daemon,
// then run lock released. Idempotent and safe to call concurrently.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		if s.listener == nil {
			return
		}
		s.closeErr = s.listener.Close()
		s.handlerWG.Wait()

		// Lifecycle teardown. Identity is removed first so a competing
		// daemon that inherits the lock after release does not refuse on
		// the basis of a stale identity it does not own.
		if s.identityWrit {
			if err := lifecycle.RemoveIdentityIfOwned(s.paths, s.identity); err != nil && !errors.Is(err, lifecycle.ErrIdentityNotOwned) {
				_ = s.logger.Log(Entry{Outcome: OutcomeIPCError, Msg: "remove identity: " + err.Error()})
			}
		}
		if s.runLock != nil {
			if err := s.runLock.Release(); err != nil {
				_ = s.logger.Log(Entry{Outcome: OutcomeIPCError, Msg: "release run lock: " + err.Error()})
			}
			s.runLock = nil
		}
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

// cleanupStaleSocket inspects the socket and identity sidecar to decide
// what to do before the caller binds. The caller already holds the run
// lock, so {socket=present, lock=held} cannot occur from another live
// daemon at this point — only an orphaned socket left behind by a crashed
// daemon (lock=free at probe time, but we already grabbed it). The four
// cells in MILESTONE_PLAN reduce here to:
//
//   - socket missing → no-op (lock=free in our hand, nothing to clean).
//   - socket present, dial succeeds → another daemon answered after we
//     grabbed the lock, which means we lost a race — refuse.
//   - socket present, dial fails definitively → orphan socket; remove
//     socket + leftover identity + cooldown so the bind can proceed.
//   - non-socket file at the path → refuse.
func (s *Server) cleanupStaleSocket() error {
	info, err := os.Stat(s.socketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Lock=held (we hold it) + socket=absent: clean state. Drop
			// any stale identity/cooldown left over from a previous
			// daemon that crashed between Listen failure and identity
			// remove.
			s.dropStaleSidecars()
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
	s.dropStaleSidecars()
	return nil
}

// dropStaleSidecars best-effort removes the identity sidecar left behind
// by a previous daemon that crashed; we hold the run lock now, so no live
// daemon owns it. Cooldown markers are intentionally NOT touched here:
// cooldown is parent-side recovery state owned by the launcher, which
// clears it on its own success path. Errors are logged and otherwise
// ignored so a transient sidecar issue does not block startup.
func (s *Server) dropStaleSidecars() {
	if s.paths.Identity == "" {
		return
	}
	if err := os.Remove(s.paths.Identity); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = s.logger.Log(Entry{Outcome: OutcomeIPCError, Msg: "remove stale identity: " + err.Error()})
	}
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
	case kindStop:
		if req.Stop == nil {
			s.writeError(conn, "stop request missing payload")
			return
		}
		resp := StopResponse{Accepted: true}
		s.writeResponse(conn, wireResponse{Kind: kindStop, Stop: &resp})
		if s.shutdownFn != nil {
			go s.shutdownFn()
		}
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
