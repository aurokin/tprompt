//go:build darwin

package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner records every invocation and returns pre-canned outputs
// in order. Lookup is by command basename + first arg so a single
// fakeRunner can model the codesign/spctl interleaving the assessor
// performs.
type fakeRunner struct {
	calls    []runCall
	canned   map[string]runResult
	missing  map[string]bool // when set, Run returns ErrNotFound for the command
	defaultR runResult
}

type runCall struct {
	cmd  string
	args []string
}

type runResult struct {
	stdout, stderr string
	exit           int
	err            error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	f.calls = append(f.calls, runCall{cmd: name, args: append([]string(nil), args...)})
	if f.missing != nil && f.missing[name] {
		return nil, nil, -1, errors.New("not found")
	}
	key := f.key(name, args)
	if r, ok := f.canned[key]; ok {
		return []byte(r.stdout), []byte(r.stderr), r.exit, r.err
	}
	r := f.defaultR
	return []byte(r.stdout), []byte(r.stderr), r.exit, r.err
}

func (*fakeRunner) key(name string, args []string) string {
	if len(args) == 0 {
		return name
	}
	// Disambiguate codesign --verify from codesign -d -vv, and from
	// spctl --assess. Using args[0] is enough since the assessor only
	// ever runs each (name, args[0]) combination once.
	return name + "/" + args[0]
}

// availablePaths returns paths that exist on every macOS host so the
// assessor's checkToolsAvailable step passes. The real codesign/spctl
// constants are not used in unit tests because we don't want unit
// tests invoking the real tools — that's what the integration tests
// in trust_darwin_integration_test.go are for.
func availablePaths() (codesign, spctl string) {
	return "/usr/bin/true", "/usr/bin/false"
}

func TestDarwinAssessorAdHocSignatureViaSignatureLine(t *testing.T) {
	t.Parallel()
	cs, sp := availablePaths()
	r := &fakeRunner{canned: map[string]runResult{
		cs + "/--verify": {exit: 0},
		cs + "/-d":       {exit: 0, stderr: "Executable=/x\nFormat=Mach-O\nSignature=adhoc\n"},
		sp + "/--assess": {exit: 0},
	}}
	a := darwinAssessor{codesign: cs, spctl: sp, getenv: func(string) string { return "" }, run: r}
	res := a.Assess(IntentImplicitTUI, "/usr/local/bin/tprompt")
	if res.Allow {
		t.Fatal("ad-hoc signature should be denied")
	}
	if !strings.Contains(res.Reason, "ad-hoc signature") {
		t.Errorf("reason = %q, want it to mention ad-hoc signature", res.Reason)
	}
	if !strings.Contains(res.Reason, "/usr/local/bin/tprompt") {
		t.Errorf("reason = %q, want path-specific", res.Reason)
	}
	if !strings.Contains(res.Reason, "tprompt daemon start") {
		t.Errorf("reason = %q, want recovery hint", res.Reason)
	}
	if !strings.Contains(res.Reason, "TPROMPT_UNSAFE_SKIP_TRUST_GATE") {
		t.Errorf("reason = %q, want override hint", res.Reason)
	}
}

func TestDarwinAssessorAdHocSignatureViaCodeDirectoryFlags(t *testing.T) {
	t.Parallel()
	cs, sp := availablePaths()
	// Linker-signed clang output: only `flags=...adhoc...`. No
	// `Signature=adhoc` line.
	stderr := "Executable=/x\nIdentifier=hello\nCodeDirectory v=20400 size=254 flags=0x20002(adhoc,linker-signed) hashes=5+0\n"
	r := &fakeRunner{canned: map[string]runResult{
		cs + "/--verify": {exit: 0},
		cs + "/-d":       {exit: 0, stderr: stderr},
		sp + "/--assess": {exit: 0},
	}}
	a := darwinAssessor{codesign: cs, spctl: sp, getenv: func(string) string { return "" }, run: r}
	res := a.Assess(IntentImplicitTUI, "/x")
	if res.Allow {
		t.Fatal("flags=adhoc should be denied")
	}
	if !strings.Contains(res.Reason, "ad-hoc signature") {
		t.Errorf("reason = %q", res.Reason)
	}
}

func TestDarwinAssessorInvalidSignature(t *testing.T) {
	t.Parallel()
	cs, sp := availablePaths()
	r := &fakeRunner{canned: map[string]runResult{
		cs + "/--verify": {exit: 1, stderr: "/x: code object is not signed at all\nIn architecture: arm64\n"},
	}}
	a := darwinAssessor{codesign: cs, spctl: sp, getenv: func(string) string { return "" }, run: r}
	res := a.Assess(IntentImplicitTUI, "/x")
	if res.Allow {
		t.Fatal("invalid signature should be denied")
	}
	if !strings.Contains(res.Reason, "invalid signature") {
		t.Errorf("reason = %q", res.Reason)
	}
	if !strings.Contains(res.Reason, "code object is not signed at all") {
		t.Errorf("reason = %q, want stderr first line", res.Reason)
	}
	// codesign -d and spctl must NOT be invoked when verify failed.
	for _, c := range r.calls {
		if c.cmd == cs && len(c.args) > 0 && c.args[0] == "-d" {
			t.Errorf("codesign -d called after verify failure: %v", c)
		}
		if c.cmd == sp {
			t.Errorf("spctl called after verify failure: %v", c)
		}
	}
}

func TestDarwinAssessorGatekeeperRejected(t *testing.T) {
	t.Parallel()
	cs, sp := availablePaths()
	r := &fakeRunner{canned: map[string]runResult{
		cs + "/--verify": {exit: 0},
		cs + "/-d":       {exit: 0, stderr: "Authority=Apple\nSignature size=4523\n"},
		sp + "/--assess": {exit: 3, stderr: "/x: rejected\nsource=Unverified\n"},
	}}
	a := darwinAssessor{codesign: cs, spctl: sp, getenv: func(string) string { return "" }, run: r}
	res := a.Assess(IntentImplicitTUI, "/x")
	if res.Allow {
		t.Fatal("Gatekeeper rejection should be denied")
	}
	if !strings.Contains(res.Reason, "Gatekeeper rejected") {
		t.Errorf("reason = %q", res.Reason)
	}
}

func TestDarwinAssessorValidCLIBypass(t *testing.T) {
	t.Parallel()
	cs, sp := availablePaths()
	// spctl says "valid but not an app" for a CLI binary; we allow.
	stderr := "/x: rejected (the code is valid but does not seem to be an app)\norigin=Apple\n"
	r := &fakeRunner{canned: map[string]runResult{
		cs + "/--verify": {exit: 0},
		cs + "/-d":       {exit: 0, stderr: "Authority=Apple\n"},
		sp + "/--assess": {exit: 3, stderr: stderr},
	}}
	a := darwinAssessor{codesign: cs, spctl: sp, getenv: func(string) string { return "" }, run: r}
	res := a.Assess(IntentImplicitTUI, "/x")
	if !res.Allow {
		t.Fatalf("CLI bypass should be allowed; reason=%q", res.Reason)
	}
}

func TestDarwinAssessorFullyAllowed(t *testing.T) {
	t.Parallel()
	cs, sp := availablePaths()
	r := &fakeRunner{canned: map[string]runResult{
		cs + "/--verify": {exit: 0},
		cs + "/-d":       {exit: 0, stderr: "Authority=Apple\n"},
		sp + "/--assess": {exit: 0},
	}}
	a := darwinAssessor{codesign: cs, spctl: sp, getenv: func(string) string { return "" }, run: r}
	res := a.Assess(IntentImplicitTUI, "/x")
	if !res.Allow {
		t.Fatalf("fully signed + Gatekeeper-accepted should allow; reason=%q", res.Reason)
	}
}

func TestDarwinAssessorToolsMissing(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{}
	a := darwinAssessor{
		codesign: "/nope/codesign",
		spctl:    "/usr/bin/true",
		getenv:   func(string) string { return "" },
		run:      r,
	}
	res := a.Assess(IntentImplicitTUI, "/x")
	if res.Allow {
		t.Fatal("missing codesign should be denied (degrade-loudly path)")
	}
	if !strings.Contains(res.Reason, "trust tools unavailable") {
		t.Errorf("reason = %q", res.Reason)
	}
	if len(r.calls) != 0 {
		t.Errorf("runner invoked despite missing tool: %v", r.calls)
	}
}

func TestDarwinAssessorOverrideShortCircuits(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"1", "true", "True", "YES", "on"} {
		v := v
		t.Run("v="+v, func(t *testing.T) {
			t.Parallel()
			cs, sp := availablePaths()
			r := &fakeRunner{}
			a := darwinAssessor{
				codesign: cs,
				spctl:    sp,
				getenv: func(k string) string {
					if k == trustOverrideEnv {
						return v
					}
					return ""
				},
				run: r,
			}
			res := a.Assess(IntentImplicitTUI, "/x")
			if !res.Allow {
				t.Fatalf("override %q should allow; reason=%q", v, res.Reason)
			}
			if !strings.Contains(res.Reason, "debug override") {
				t.Errorf("reason = %q, want 'debug override'", res.Reason)
			}
			if !strings.Contains(res.Reason, v) {
				t.Errorf("reason = %q, want literal %q", res.Reason, v)
			}
			if len(r.calls) != 0 {
				t.Errorf("runner invoked despite debug override: %v", r.calls)
			}
		})
	}
}

func TestDarwinAssessorOverrideKnownNegativeKeepsGate(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"0", "false", "no", "off", "bogus"} {
		v := v
		t.Run("v="+v, func(t *testing.T) {
			t.Parallel()
			cs, sp := availablePaths()
			// Wire a fully-allowed flow so we can confirm the gate ran
			// (rather than short-circuiting to allow due to mistaken
			// override interpretation).
			r := &fakeRunner{canned: map[string]runResult{
				cs + "/--verify": {exit: 0},
				cs + "/-d":       {exit: 0, stderr: "Authority=Apple\n"},
				sp:               {exit: 0},
			}}
			a := darwinAssessor{
				codesign: cs,
				spctl:    sp,
				getenv: func(k string) string {
					if k == trustOverrideEnv {
						return v
					}
					return ""
				},
				run: r,
			}
			res := a.Assess(IntentImplicitTUI, "/x")
			if !res.Allow {
				t.Fatalf("known-negative override %q should leave gate active and reach allow path; reason=%q", v, res.Reason)
			}
			if strings.Contains(res.Reason, "debug override") {
				t.Errorf("reason = %q, must NOT mention debug override", res.Reason)
			}
			if len(r.calls) == 0 {
				t.Errorf("runner not invoked despite known-negative value %q", v)
			}
		})
	}
}

func TestDarwinAssessorIsAdHoc(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"signature line", "Signature=adhoc", true},
		{"flags only", "CodeDirectory v=20400 flags=0x20002(adhoc,linker-signed) ...", true},
		{"both", "Signature=adhoc\nCodeDirectory v=20400 flags=0x2(adhoc) ...", true},
		{"developer signed", "Authority=Developer ID Application: Aurokin\nSignature size=4523", false},
		{"unrelated adhoc word", "Note: adhoc development workflow", false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := isAdHoc([]byte(c.in))
			if got != c.want {
				t.Errorf("isAdHoc(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestDarwinAssessorIsCLIBypass(t *testing.T) {
	t.Parallel()
	if !isCLIBypass([]byte("/usr/bin/git: rejected (the code is valid but does not seem to be an app)\norigin=Software Signing\n")) {
		t.Error("git stderr should match CLI bypass")
	}
	if isCLIBypass([]byte("/x: rejected\nsource=Unverified\n")) {
		t.Error("plain rejection must not trigger CLI bypass")
	}
}

func TestDarwinAssessorFirstLine(t *testing.T) {
	t.Parallel()
	if got := firstLine([]byte("\n\n  hello \n  world\n")); got != "hello" {
		t.Errorf("firstLine = %q, want hello", got)
	}
	if got := firstLine(nil); got != "" {
		t.Errorf("firstLine(nil) = %q, want empty", got)
	}
}
