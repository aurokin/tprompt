package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestZeroArgCommandsRejectExtraOperands(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "list", args: []string{"list", "extra"}},
		{name: "paste", args: []string{"paste", "extra"}},
		{name: "doctor", args: []string{"doctor", "extra"}},
		{name: "tui", args: []string{"tui", "extra"}},
		{name: "pick", args: []string{"pick", "extra"}},
		{name: "daemon start", args: []string{"daemon", "start", "extra"}},
		{name: "daemon status", args: []string{"daemon", "status", "extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := executeRoot(t, tt.args...)
			if err == nil {
				t.Fatal("want usage error, got nil")
			}
			if errors.Is(err, ErrNotImplemented) {
				t.Fatalf("want args validation error, got handler error %v", err)
			}
			if !strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("want cobra usage error, got %v", err)
			}
		})
	}
}

func TestZeroArgCommandsAcceptBareInvocation(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "list", args: []string{"list"}},
		{name: "paste", args: []string{"paste"}},
		{name: "doctor", args: []string{"doctor"}},
		{name: "tui", args: []string{"tui"}},
		{name: "pick", args: []string{"pick"}},
		{name: "daemon start", args: []string{"daemon", "start"}},
		{name: "daemon status", args: []string{"daemon", "status"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := executeRoot(t, tt.args...)
			if !errors.Is(err, ErrNotImplemented) {
				t.Fatalf("want ErrNotImplemented, got %v", err)
			}
		})
	}
}

func executeRoot(t *testing.T, args ...string) (stdout string, stderr string, err error) {
	t.Helper()

	root := NewRootCmd(fakeDeps(t))
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}
