//go:build darwin

package lifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise the real /usr/bin/codesign and /usr/bin/spctl
// against fixture binaries. They're the only way to catch drift when
// macOS changes codesign/spctl output formats — a fake-runner unit
// test can only verify what the test author thought the format would
// be.
//
// All tests skip if the corresponding tool is missing.

func skipIfToolsMissing(t *testing.T) {
	t.Helper()
	for _, p := range []string{codesignPath, spctlPath} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("required tool %s not available: %v", p, err)
		}
	}
}

func TestDarwinAssessorIntegrationGitAllow(t *testing.T) {
	t.Parallel()
	skipIfToolsMissing(t)
	if _, err := os.Stat("/usr/bin/git"); err != nil {
		t.Skipf("/usr/bin/git not available: %v", err)
	}
	a := darwinAssessor{
		codesign: codesignPath,
		spctl:    spctlPath,
		run:      execRunner{},
	}
	res := a.Assess(IntentImplicitTUI, "/usr/bin/git")
	if !res.Allow {
		t.Fatalf("/usr/bin/git should be allowed (notarized, CLI-bypass branch); reason=%q", res.Reason)
	}
}

func TestDarwinAssessorIntegrationAdHocReject(t *testing.T) {
	t.Parallel()
	skipIfToolsMissing(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skipf("clang not available: %v", err)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "hello.c")
	bin := filepath.Join(dir, "hello")
	if err := os.WriteFile(src, []byte("int main(void){return 0;}\n"), 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if out, err := exec.Command(clang, "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("clang build: %v\n%s", err, out)
	}
	// On Apple Silicon, clang produces a linker-signed (ad-hoc) binary
	// by default. On Intel macOS, the binary is not signed at all; we
	// then ad-hoc sign it explicitly so we always exercise the
	// adhoc-detection branch (rather than the unsigned branch).
	if out, err := exec.Command(codesignPath, "-fs", "-", bin).CombinedOutput(); err != nil {
		t.Fatalf("codesign -fs -: %v\n%s", err, out)
	}

	a := darwinAssessor{
		codesign: codesignPath,
		spctl:    spctlPath,
		run:      execRunner{},
	}
	res := a.Assess(IntentImplicitTUI, bin)
	if res.Allow {
		t.Fatalf("ad-hoc binary should be denied; reason=%q", res.Reason)
	}
	if !strings.Contains(res.Reason, "ad-hoc signature") {
		t.Errorf("reason = %q, want it to mention ad-hoc signature", res.Reason)
	}
}
