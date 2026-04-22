package daemon

import (
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/hsadler/tprompt/internal/testutil"
)

func TestSocketClientStatusReadFailureReturnsIPCError(t *testing.T) {
	path := filepath.Join(testutil.ShortTempDir(t), "daemon.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err == nil {
			var req wireRequest
			_ = json.NewDecoder(conn).Decode(&req)
			_ = conn.Close()
		}
	}()

	client := NewSocketClient(path)
	_, err = client.Status()
	<-done

	var ipcErr *IPCError
	if !errors.As(err, &ipcErr) {
		t.Fatalf("want IPCError, got %T: %v", err, err)
	}
	if ipcErr.Op != "read response" {
		t.Fatalf("Op = %q, want read response", ipcErr.Op)
	}
	if ipcErr.Path != path {
		t.Fatalf("Path = %q, want %q", ipcErr.Path, path)
	}
}
