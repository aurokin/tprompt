# ISSUE_PLAN — AUR-314 macOS implicit auto-start trust gate

## Goal

Wire the `TrustAssessor` seam (introduced in AUR-265 as a no-op) to a
real macOS executable-trust preflight that runs only for implicit
auto-start. Goals are inherited from the milestone plan §3:

- Reject ad-hoc signed binaries.
- Reject invalid (unsigned, tampered) code signatures.
- Reject Gatekeeper-rejected binaries.
- Allow validly signed CLI binaries that Gatekeeper labels "valid but
  not an app" (the common case for our binary).
- `TPROMPT_UNSAFE_SKIP_TRUST_GATE=1` short-circuits before any
  `codesign`/`spctl` invocation.
- Explicit `daemon start` and `daemon run` keep bypassing the gate
  (already implemented via `StartIntent`).
- Failure detail is path-specific and only suggests flags/commands the
  invoking command actually accepts.
- Compiled only on darwin where it runs; non-darwin gets a no-op
  assessor.

## Files

New:

- `internal/app/lifecycle/trust_darwin.go` — `//go:build darwin`. Exports
  `ProductionAssessor() TrustAssessor` returning a `darwinAssessor`
  that consults `codesign` and `spctl` through an injectable runner.
  Format failure detail with a path-specific recovery line that names
  `tprompt daemon start` / `tprompt daemon run` and the env var.
- `internal/app/lifecycle/trust_other.go` — `//go:build !darwin`.
  Exports `ProductionAssessor() TrustAssessor` returning the existing
  `noopAssessor{}`. One-line comment notes the override env var is
  darwin-only.
- `internal/app/lifecycle/trust_darwin_test.go` — `//go:build darwin`.
  Table-driven unit tests using a fake `runner` that pre-cans
  codesign/spctl invocations: ad-hoc, invalid signature,
  Gatekeeper-rejected, valid CLI ("not an app" case),
  validly-signed-and-Gatekeeper-accepted, debug-override short-circuit
  (asserts no runner calls happened), debug-override-with-known-negative
  (asserts gate stays active).
- `internal/app/lifecycle/trust_darwin_integration_test.go` —
  `//go:build darwin`. Tests against the real `/usr/bin/codesign` and
  `/usr/bin/spctl`. Each test `t.Skip`s if the corresponding tool
  binary is missing. Cases:
  1. `/usr/bin/git` (notarized Apple binary) → Allow (or rejected-as-CLI
     fallback Allow on hosts where spctl rejects it as not-an-app).
  2. The current go test binary (`os.Executable()`) → Allow when the
     test binary is signed validly; otherwise we just assert the
     decision is reasoned (Allow for valid CLI, Reject otherwise).
  3. A freshly-built ad-hoc binary (`clang -o /tmp/x x.c` then
     `codesign --remove-signature && codesign -fs - /tmp/x`) →
     RejectAdHoc. This is the load-bearing case and is the one most
     likely to drift if a future macOS release changes codesign
     output.
     The test skips if `clang` is missing.

Edit:

- `internal/app/lifecycle/launcher.go` — no behavior change.
- `internal/app/deps.go` — `productionNewLauncher` sets
  `Assessor: applife.ProductionAssessor()`.

## Trust signal interpretation

The assessor consults two macOS tools by absolute path
(`/usr/bin/codesign`, `/usr/bin/spctl`) — both are part of the macOS
base system on every supported version. We do not honor `$PATH` for
these, so a user mucking with their PATH cannot bypass the gate.

If either binary is missing (developer stripped /usr/bin) we ALLOW
with a reason of `trust tools unavailable`. This degradation is
documented in the failure-detail message so an operator sees what
happened. Rationale: macOS base system always ships these binaries;
absence implies an exotic host where the user has opted out of
standard tooling, and we'd rather not lock them out of auto-start.

Order: codesign verify → codesign -d -vv → spctl. We keep this order
because for the developer-signed common case, spctl will reject with
"valid but not an app", which means we'd fall through anyway; running
codesign first lets us reject the unsigned/ad-hoc/tampered cases
without touching spctl. Documented inline in `trust_darwin.go`.

1. `codesign --verify --strict <exec>` — exit 0 means the signature
   verifies cryptographically. Non-zero is `RejectInvalidSignature`
   (covers "code object is not signed at all" and tampering). The
   stderr first line is captured into the reason for diagnostics.
   `--deep` is intentionally not used; per `man codesign`, it is for
   nested code (frameworks/dylibs in app bundles), and using it on a
   standalone Mach-O CLI is a no-op or worse.

2. `codesign -d -vv <exec>` — describes the signing identity. We parse
   stderr for either of two ad-hoc markers:
   - `Signature=adhoc` literal line, OR
   - `flags=...adhoc...` substring on the `CodeDirectory` line (e.g.,
     `flags=0x20002(adhoc,linker-signed)`).
   Either match → `RejectAdHoc`. We match BOTH because clang's
   linker-signed output emits both lines, but other code paths (e.g.,
   `codesign -fs - <path>` retroactively applied) emit only one.
   We do NOT use "missing Authority=" as an ad-hoc signal because a
   developer-signed binary with a self-signed identity has Authority
   lines but is still validly signed and not ad-hoc — that case
   should fall through to spctl.

3. `spctl --assess --type execute -vv <exec>` — Gatekeeper.
   - exit 0 → `Allow`.
   - exit non-zero AND stderr contains literal
     `the code is valid but does not seem to be an app` → `Allow`
     (CLI bypass; matches the format on macOS 14/15 verified
     empirically against `/usr/bin/git` and `/bin/sh`).
   - otherwise → `RejectGatekeeper`, with the first stderr line as
     the reason detail.
   We do not pass `--ignore-cache`. Matching OS behavior is the right
   default; developers flipping signatures during testing can opt out
   with the env-var override.

## Debug override

`TPROMPT_UNSAFE_SKIP_TRUST_GATE` value handling (value-required, per
MILESTONE_PLAN.md §3):

- Empty / unset → gate is active.
- Case-insensitive `1`, `true`, `yes`, `on` → bypass with a "debug
  override" reason. Short-circuits BEFORE any runner invocation.
- Any other non-empty value (including `0`, `false`, `no`, `off`,
  garbage) → gate stays active. We log a warning at the assessor
  reason boundary so the operator sees the literal value they set.

The short-circuit is unit-testable because the runner is injectable
and the override path returns `Allow` with `runner.calls == 0`.

## Failure-detail copy

When `Assess` rejects, the launcher sets
`StartResult.Reason = ReasonTrustGate` and
`StartResult.Detail = <assessor reason>`. The assessor reason is:

> `daemon executable %q failed macOS trust check (%s). Run 'tprompt daemon start' to start the daemon explicitly, or set TPROMPT_UNSAFE_SKIP_TRUST_GATE=1 for local builds. See %s for details.`

Where `%s` is one of: `ad-hoc signature`, `invalid signature: <stderr first line>`, `Gatekeeper rejected: <stderr first line>`, `trust tools unavailable: <which one>`. The third `%s` is the daemon log path (mirrors the cooldown error format already used in launcher.go:262-263). When LogPath is empty we omit the trailing "See ... for details." sentence.

The string suggests `tprompt daemon start` (a separate command). It does NOT suggest TUI flags, because no TUI flag exists to disable the gate (the gate is a property of implicit auto-start, not a user knob).

stderr captures collapse internal newlines to spaces and trim runs
of whitespace so the failure reason fits on one logfmt line in the
pre-spawn diagnostic and one ANSI line in the TUI failure banner.

## Test plan

Unit (mock runner):
- Each darwinAssessor branch: ad-hoc (both signal variants),
  unsigned/invalid, Gatekeeper-rejected (non-CLI), CLI-bypass,
  fully-allowed, codesign-missing, spctl-missing, debug-override-true,
  debug-override-1, debug-override-yes, debug-override-FALSE (gate
  active), debug-override-bogus (gate active).
- runner.calls assertions for the override cases.
- Reason strings include the executable path, name `tprompt daemon
  start`, name the env var.

Integration (real tools, build-tagged + skipped when missing):
- `/usr/bin/git` → Allow (CLI bypass branch in practice).
- Freshly-built clang ad-hoc binary → RejectAdHoc.

Launcher-level: `TestLauncherTrustGateRejectionImplicitOnly` already
covers the StartIntent gating with stubAssessor. No change needed.

## Risks / open questions

- `codesign`/`spctl` output formats are stable on macOS 14/15 (verified
  empirically). If a future release changes them, the integration test
  will catch it before the unit tests do — that's the whole point of
  M5.
- An adversary controlling a co-installed binary path could rename
  it to look like ours, but they can't tamper with the binary
  contents without breaking codesign verify. The trust gate is about
  signature integrity, not anti-tampering of the launching tprompt
  process itself.

## Out of scope

- AUR-266 wires the launcher into the TUI default-on path.
- AUR-270 adds doctor warning + DECISIONS entry for the override env var.
- Caching the assessor result across invocations.
