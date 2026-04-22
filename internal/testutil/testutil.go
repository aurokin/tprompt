package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// ShortTempDir creates a temporary directory under a short absolute root so
// Unix-domain socket paths stay within platform limits on macOS and Linux.
func ShortTempDir(tb testing.TB) string {
	tb.Helper()

	root := shortTempRoot()
	dir, err := os.MkdirTemp(root, "tp-")
	if err != nil {
		tb.Fatalf("MkdirTemp(%q): %v", root, err)
	}
	tb.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func shortTempRoot() string {
	if info, err := os.Stat("/tmp"); err == nil && info.IsDir() {
		return "/tmp"
	}
	return filepath.Clean(os.TempDir())
}
