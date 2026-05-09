//go:build darwin

package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultTrustGateCommandTimeout bounds each codesign/spctl invocation
// so a stuck OS security tool cannot pin the launcher's start lock
// indefinitely. The two tools normally return in well under a second;
// 5s is a generous ceiling that fails fast on a hung subprocess while
// tolerating occasional cold-cache slow paths (e.g., spctl consulting
// the notarization service on first run).
const DefaultTrustGateCommandTimeout = 5 * time.Second

// codesignPath and spctlPath are absolute. macOS ships codesign at
// /usr/bin and spctl at /usr/sbin on every supported version, so
// honoring $PATH would only let an attacker plant a shim ahead of the
// real tools.
const (
	codesignPath = "/usr/bin/codesign"
	spctlPath    = "/usr/sbin/spctl"
)

// trustOverrideEnv, when set to a known-positive value, short-circuits
// the assessor with a "debug override" reason. Documented in
// MILESTONE_PLAN.md §3 and (will be) DECISIONS.md after AUR-270.
const trustOverrideEnv = "TPROMPT_UNSAFE_SKIP_TRUST_GATE"

// runner is the seam used by darwinAssessor to invoke codesign/spctl.
// Production wires execRunner; tests inject a fake.
type runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, exitCode int, err error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
			err = nil
		}
	}
	return stdout.Bytes(), stderr.Bytes(), exit, err
}

// darwinAssessor implements TrustAssessor on macOS by consulting
// codesign and spctl. See ISSUE_PLAN.md (AUR-314) for the algorithm
// and rationale; the short version: codesign verify catches unsigned
// and tampered binaries; codesign -d -vv catches ad-hoc; spctl
// classifies the rest. Validly signed CLI binaries that spctl rejects
// as "not an app" are allowed.
type darwinAssessor struct {
	codesign string
	spctl    string
	getenv   func(string) string
	run      runner
	// timeout bounds each codesign/spctl invocation. A zero value
	// disables the bound (used by unit tests with synchronous fake
	// runners). Production wires DefaultTrustGateCommandTimeout so a
	// stuck OS security tool cannot pin the launcher's start lock
	// indefinitely while implicit auto-start callers queue behind it.
	timeout time.Duration
}

// ProductionAssessor returns the macOS trust assessor wired to the
// real /usr/bin/codesign and /usr/sbin/spctl. Non-darwin builds get a
// no-op via trust_other.go. The launcher (not the assessor) appends
// the daemon log path to the failure message, so this constructor
// takes no arguments.
func ProductionAssessor() TrustAssessor {
	return darwinAssessor{
		codesign: codesignPath,
		spctl:    spctlPath,
		getenv:   os.Getenv,
		run:      execRunner{},
		timeout:  DefaultTrustGateCommandTimeout,
	}
}

// Assess implements TrustAssessor. The launcher only calls this for
// IntentImplicitTUI; other intents are bypassed in
// Launcher.runTrustGate.
func (a darwinAssessor) Assess(intent StartIntent, exec string) AssessResult {
	if r, ok := a.evaluateOverride(); ok {
		return r
	}
	if r, ok := a.checkToolsAvailable(exec); !ok {
		return r
	}
	ctx := context.Background()
	if r, ok := a.runCodesignVerify(ctx, exec); !ok {
		return r
	}
	if r, ok := a.runCodesignDescribe(ctx, exec); !ok {
		return r
	}
	if r, ok := a.runSpctlAssess(ctx, exec); !ok {
		return r
	}
	return AssessResult{Allow: true}
}

// evaluateOverride short-circuits the gate when the env var is set to
// a known-positive value. Known-negative values (`0`, `false`, ...)
// leave the gate active. The decision is logged into the AssessResult
// reason field on the bypass path so operators can see what they set.
func (a darwinAssessor) evaluateOverride() (AssessResult, bool) {
	v := strings.TrimSpace(a.getenv(trustOverrideEnv))
	if v == "" {
		return AssessResult{}, false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return AssessResult{Allow: true, Reason: trustOverrideEnv + "=" + v + " (debug override)"}, true
	}
	// Unknown value: gate stays active. We surface the literal value
	// later if the gate denies, so an operator sees they typed `0`
	// instead of unsetting the var.
	return AssessResult{}, false
}

func (a darwinAssessor) checkToolsAvailable(execPath string) (AssessResult, bool) {
	for _, p := range []string{a.codesign, a.spctl} {
		if _, err := os.Stat(p); err != nil {
			return a.deny(execPath, "trust tools unavailable: "+p), false
		}
	}
	return AssessResult{}, true
}

// runBounded invokes the runner under a per-command timeout when one
// is configured. The bound is critical for the trust gate: the start
// lock is held while Assess is in flight, so a hung codesign/spctl
// would otherwise block every other implicit auto-start caller.
func (a darwinAssessor) runBounded(parent context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	if a.timeout <= 0 {
		return a.run.Run(parent, name, args...)
	}
	ctx, cancel := context.WithTimeout(parent, a.timeout)
	defer cancel()
	return a.run.Run(ctx, name, args...)
}

// classifyRunErr produces a human-readable reason for run errors,
// distinguishing timeouts (so operators see "timed out after 5s"
// rather than the opaque "context deadline exceeded") from other
// invocation failures.
func (a darwinAssessor) classifyRunErr(err error, label string) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("%s timed out after %s", label, a.timeout)
	}
	return label + " failed to run: " + err.Error()
}

func (a darwinAssessor) runCodesignVerify(ctx context.Context, execPath string) (AssessResult, bool) {
	_, stderr, code, err := a.runBounded(ctx, a.codesign, "--verify", "--strict", execPath)
	if err != nil {
		return a.deny(execPath, a.classifyRunErr(err, "codesign verify")), false
	}
	if code == 0 {
		return AssessResult{}, true
	}
	return a.deny(execPath, "invalid signature: "+firstLine(stderr)), false
}

func (a darwinAssessor) runCodesignDescribe(ctx context.Context, execPath string) (AssessResult, bool) {
	_, stderr, code, err := a.runBounded(ctx, a.codesign, "-d", "-vv", execPath)
	if err != nil {
		return a.deny(execPath, a.classifyRunErr(err, "codesign describe")), false
	}
	if code != 0 {
		// Describe should not normally fail when verify passed; if it
		// does we treat it like an invalid signature for safety.
		return a.deny(execPath, "codesign describe rejected: "+firstLine(stderr)), false
	}
	if isAdHoc(stderr) {
		return a.deny(execPath, "ad-hoc signature"), false
	}
	return AssessResult{}, true
}

func (a darwinAssessor) runSpctlAssess(ctx context.Context, execPath string) (AssessResult, bool) {
	_, stderr, code, err := a.runBounded(ctx, a.spctl, "--assess", "--type", "execute", "-vv", execPath)
	if err != nil {
		return a.deny(execPath, a.classifyRunErr(err, "spctl")), false
	}
	if code == 0 {
		return AssessResult{}, true
	}
	if isCLIBypass(stderr) {
		return AssessResult{}, true
	}
	return a.deny(execPath, "Gatekeeper rejected: "+firstLine(stderr)), false
}

// isAdHoc returns true when codesign -d -vv stderr indicates an
// ad-hoc signature. We match BOTH known signal variants because clang's
// linker-signed output emits both, but other paths
// (codesign -fs - <path>) emit only one.
func isAdHoc(stderr []byte) bool {
	s := string(stderr)
	if strings.Contains(s, "Signature=adhoc") {
		return true
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "CodeDirectory ") && strings.Contains(line, "adhoc") {
			return true
		}
	}
	return false
}

// isCLIBypass returns true when spctl rejected the binary with the
// "valid but not an app" message. Verified empirically on macOS 14/15
// against /usr/bin/git and /bin/sh, which produce:
//
//	<path>: rejected (the code is valid but does not seem to be an app)
//	origin=Software Signing
//
// The match string is "the code is valid but does not seem to be an
// app" — note "an app", not "an app bundle".
func isCLIBypass(stderr []byte) bool {
	return bytes.Contains(stderr, []byte("the code is valid but does not seem to be an app"))
}

func (a darwinAssessor) deny(execPath, reason string) AssessResult {
	// The daemon log path is appended by launcherFailureMessage in
	// internal/app/tui.go so we don't end up with two copies of it
	// when the failure surfaces in the TUI banner.
	msg := fmt.Sprintf(
		"daemon executable %q failed macOS trust check (%s). Run 'tprompt daemon start' to start the daemon explicitly, or set %s=1 for local builds.",
		execPath, reason, trustOverrideEnv,
	)
	return AssessResult{Allow: false, Reason: msg}
}

// firstLine returns the first non-empty line of buf with surrounding
// whitespace trimmed. Used to keep stderr fragments single-line so
// they fit in the launcher's logfmt diagnostic and the TUI failure
// banner without wrapping.
func firstLine(buf []byte) string {
	for _, raw := range strings.Split(string(buf), "\n") {
		line := strings.TrimSpace(raw)
		if line != "" {
			return line
		}
	}
	return ""
}
