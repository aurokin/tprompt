package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// DefaultDialTimeout caps how long the client waits for the daemon to accept
// a connection. Local Unix socket connect should be sub-millisecond; this
// exists to bound hangs against a wedged daemon.
const DefaultDialTimeout = 200 * time.Millisecond

// DefaultClientReadTimeout caps how long a single request/response cycle can
// take. Submit is fire-and-ack so the daemon should respond immediately; the
// timeout protects against a stuck handler.
const DefaultClientReadTimeout = 5 * time.Second

// NewSocketClient returns a Client that talks to the daemon over a Unix
// domain socket at the given path. The returned client opens a fresh
// connection for each call (the protocol is one request per connection).
func NewSocketClient(path string) Client {
	return &socketClient{
		path:        path,
		dialTimeout: DefaultDialTimeout,
		readTimeout: DefaultClientReadTimeout,
	}
}

type socketClient struct {
	path        string
	dialTimeout time.Duration
	readTimeout time.Duration
}

func (c *socketClient) Submit(req SubmitRequest) (SubmitResponse, error) {
	var resp wireResponse
	if err := c.do(wireRequest{Kind: kindSubmit, Submit: &req}, &resp); err != nil {
		return SubmitResponse{}, err
	}
	if resp.Error != "" {
		return SubmitResponse{}, fmt.Errorf("daemon: %s", resp.Error)
	}
	if resp.Submit == nil {
		return SubmitResponse{}, errors.New("daemon: empty submit response")
	}
	return *resp.Submit, nil
}

func (c *socketClient) Status() (StatusResponse, error) {
	req := StatusRequest{}
	var resp wireResponse
	if err := c.do(wireRequest{Kind: kindStatus, Status: &req}, &resp); err != nil {
		return StatusResponse{}, err
	}
	if resp.Error != "" {
		return StatusResponse{}, fmt.Errorf("daemon: %s", resp.Error)
	}
	if resp.Status == nil {
		return StatusResponse{}, errors.New("daemon: empty status response")
	}
	return *resp.Status, nil
}

func (c *socketClient) do(req wireRequest, resp *wireResponse) error {
	conn, err := net.DialTimeout("unix", c.path, c.dialTimeout)
	if err != nil {
		return &SocketUnavailableError{Path: c.path, Reason: dialReason(err)}
	}
	defer func() { _ = conn.Close() }()

	if c.readTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.readTimeout))
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return &IPCError{Path: c.path, Op: "write request", Reason: err.Error()}
	}
	if err := json.NewDecoder(conn).Decode(resp); err != nil {
		return &IPCError{Path: c.path, Op: "read response", Reason: err.Error()}
	}
	return nil
}

// dialReason normalizes connection-level failures so SocketUnavailableError
// reports something useful (e.g. "connection refused" or "no such file")
// without leaking the full *net.OpError chain.
func dialReason(err error) string {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		return opErr.Err.Error()
	}
	return err.Error()
}
