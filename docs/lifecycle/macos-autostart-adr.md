# ADR: macOS Daemon Auto-Start And Executable Assessment

Status: accepted
Date: 2026-05-10

This ADR is the durable record of why `tprompt` does not implicitly
auto-start its daemon on macOS. It is adapted from agentscan's
[`macos-daemon-autostart-adr.md`](https://github.com/aurokin/agentscan/blob/main/docs/notes/macos-daemon-autostart-adr.md)
to reflect tprompt's TUI-driven daemon-handoff shape; agentscan and
tprompt run on the same hosts, hit the same kernel pathway, and adopt
the same accepted decision.

## Context

`tprompt` is a tmux-popup TUI that delivers prompts and clipboard
content into a target tmux pane. The signature flow is:

```text
tmux popup -> tprompt tui -> daemon socket -> verified focus -> inject
```

The daemon runs in the background, accepts delivery jobs from the
TUI, waits for tmux focus to return to the originating pane, then
runs the sanitizer and the tmux paste command. The daemon is required
because in the popup launch, tmux will not return focus to the target
pane until the TUI process exits — the TUI cannot do the injection
itself.

The default-on auto-start rollout (AUR-183) added a launcher seam: a
TUI invocation that found the daemon socket unreachable would call
the lifecycle launcher to spawn a detached `tprompt daemon run`
child, wait for readiness, and proceed. The detached child is
launched via `cmd.Start()` with `Setsid: true`, which is structurally
identical to the agentscan path.

## Observed Failure (agentscan, replicable host class)

After the auto-start rollout in agentscan on a shared development
host, the host kernel-panicked repeatedly with the same signature:

- panic: `os_refcnt: overflow ... @refcnt.c`
- panicked task: the agent binary (analogous to `tprompt`)
- kernel backtrace: `com.apple.AppleSystemPolicy` with AMFI,
  quarantine, and sandbox dependencies
- unified log context: `syspolicyd` and `amfid` doing signature,
  trust, Gatekeeper, provenance, and detached-signature work

The panicked process had essentially no userspace runtime state: zero
user time, zero system time, one main thread in kernel frames, and a
tiny resident set. That points to macOS launch / policy evaluation,
not application logic.

Observed panic files (agentscan host, included here as the same
hardware also runs tprompt):

- `/Library/Logs/DiagnosticReports/panic-full-2026-05-06-170918.0002.panic`
- `/Library/Logs/DiagnosticReports/panic-full-2026-05-07-084149.0002.panic`
- `/Library/Logs/DiagnosticReports/panic-full-2026-05-07-212349.0002.panic`
- `/Library/Logs/DiagnosticReports/panic-full-2026-05-08-071032.0002.panic`

Post-crash inspection of installed agent binaries found ad-hoc /
linker-signed binaries carrying `com.apple.provenance`. The attribute
could not be removed with `xattr -d` or `xattr -c`.

## Why this also applies to tprompt

tprompt's auto-start path is structurally the same as agentscan's: a
short-lived foreground process resolves its own executable path, runs
a trust preflight, and `Setsid`-detaches a child running
`<binary> daemon run`. The detached child enters the same macOS
launch evaluation pipeline — `AppleSystemPolicy` + AMFI +
`syspolicyd` — that produced the agentscan panic class. Both tools
ship as `darwin/arm64` Go binaries from the same release pipeline
and are installed by the same operators on the same hosts. There is
no engineering reason to expect tprompt to be exempt from the kernel
failure mode.

No tprompt-attributed panic file exists because the policy here was
adopted preemptively: the agentscan rollout panicked the host before
tprompt's default-on auto-start landed in a tagged release, so
tprompt's auto-start was disabled on darwin (AUR-326) before any
tprompt binary could trigger the same bug. The provenance of the
kernel evidence is therefore agentscan, but the structural identity
of the two launch paths makes the carryover engineering-sound.

The failure is a macOS kernel failure in the policy path. Even if the
executable is ad-hoc signed, invalidly signed, quarantined, or
provenance-tagged, correct OS behavior should be launch denial or an
error, not a host panic. Because the panic appears to happen during
process launch or assessment, Go guards inside the child process
cannot be relied on: a child that panics before userspace
initialization will not log, reject, or clean up through application
code.

## Accepted Decision

Detached daemon auto-start on macOS has a stricter product boundary
than Linux:

1. A normal foreground tprompt invocation (including bare `tprompt`
   dispatching into TUI inside tmux+tty) MUST NOT silently self-exec
   a detached daemon on macOS.
2. A macOS child process MUST NOT be the first place where unsafe
   daemon startup is rejected; the parent decides before spawning.
3. Foreground `tprompt daemon run` is the supported recovery and
   development path for macOS users.
4. Explicit detached `tprompt daemon start` remains available on
   macOS only for non-ad-hoc, validly signed binaries; the parent
   runs an executable-trust preflight (codesign verify, ad-hoc
   detection, Gatekeeper assess with CLI bypass) before the spawn,
   and rejects with a recovery hint when the gate fails.
5. The user has a hard opt-out from auto-start via the
   `--no-daemon-auto-start` flag and `TPROMPT_NO_AUTO_START=1` env
   var. On darwin these short-circuit the auto-start attempt
   upstream — concretely in `runTUI` before `LoadConfig`, see the
   [Opt-outs section in `auto-start.md`](auto-start.md) for the
   call-site and conflict-handling details. They do not re-enable
   the implicit path because the policy is hardcoded.
6. Release signing covers `darwin/arm64` only; other macOS targets
   are not currently distributed as signed binaries.

## Selected Alternative

### Option B — no implicit auto-start on macOS (accepted)

TUI flows on macOS connect to an existing daemon. If no daemon is
running, the TUI refuses with a single actionable line naming
`tprompt daemon run` (foreground, long-lived tmux pane) and
`tprompt daemon start` (detached, signed release) as recovery
commands. The "Implemented Policy" section below describes how the
boundary is enforced for each operator persona (signed-release,
ad-hoc/local-build, TUI-only user).

## Rejected Alternatives

### Option A — signed-only detached daemon on macOS

Allow implicit auto-start when the binary is Developer ID signed +
notarized. Rejected: this is too permissive for implicit starts. The
panic observed in agentscan happened during the OS policy evaluation
itself, not after a clean signature check. Adding more policy work
in the implicit path is the opposite of what the kernel evidence
requires.

### Option C — foreground helper instead of detached self-exec

The TUI runs in foreground long enough to do the work itself.
Rejected because tmux popup focus does not return to the target pane
until the TUI exits — the TUI cannot inject. The whole point of the
daemon is to wait for verified focus after the TUI is gone. A
foreground helper would re-introduce the brittle fixed-sleep model
that the daemon replaced.

### Option D — keep auto-start but add richer assessment and audit logging

Preserves the Linux-like UX. Rejected because the failing component
is the macOS policy path itself. Adding more assessment exercises
the same code path more, not less. Audit logging would only help
post-mortem; it would not prevent the panic.

## Implemented Policy

- macOS signed release binary: explicit detached `tprompt daemon
  start` is allowed (after passing the trust preflight), but
  implicit auto-start remains disabled. This is the narrow form of
  Option A's "signed-only detached daemon" idea — explicit user
  intent + signed binary = allowed — without extending the
  permissive surface to implicit starts.
- macOS ad-hoc or locally built binary: detached starts are
  rejected; users run `tprompt daemon run` in a long-lived tmux
  pane, or run `scripts/sign-macos-binary.sh` to self-sign a local
  dev build.
- `tprompt tui` on macOS requires an already-running daemon. When
  the socket is unreachable, the refusal line states the daemon is
  not running, explains macOS does not implicitly auto-start, and
  names both recovery commands.
- The `TPROMPT_UNSAFE_TRUST_PREFLIGHT_BYPASS=1` env var disables
  the trust gate for the testscript suite (where the test binary is
  ad-hoc-signed by `go test`) and for local development. It is
  permitted only in those contexts; release operation must not set
  it.

## Mapping To Code

- `internal/app/lifecycle/policy_darwin.go` — `MacOSImplicitAutoStartDisabled`
  refuses `IntentImplicitTUI` on darwin and renders the refusal
  reason mirrored from agentscan.
- `internal/app/lifecycle/launcher.go` — the launcher applies the
  policy before any cooldown / start-lock / spawn work; for
  `IntentExplicitStart` it then runs the trust gate.
- `internal/app/lifecycle/trust_darwin.go` — `darwinAssessor`
  runs codesign verify, ad-hoc detection, and Gatekeeper assess
  with CLI bypass; intent-aware deny rendering matches the
  operator's actual command.
- `internal/app/commands.go` — `preflightDaemonRun` runs the same
  assessor on the foreground `daemon run` entrypoint with
  `IntentExplicitRun` so users invoking `daemon run` directly are
  also gated.
- `internal/app/tui.go` — `runTUI` resolves the auto-start opt-outs
  via `resolveTUIAutoStartIntent` (Accepted Decision §5) before
  `LoadConfig`, then `autoStartTUIDaemon` short-circuits the
  implicit path on darwin before constructing the launcher,
  surfacing the policy refusal through the standard daemon IPC
  error wrapper.

## Cross-References

- agentscan ADR (source): [`macos-daemon-autostart-adr.md`](https://github.com/aurokin/agentscan/blob/main/docs/notes/macos-daemon-autostart-adr.md)
- DECISIONS [§33 — Daemon lifecycle architecture](../../DECISIONS.md)
- EXPECTATIONS [Daemon Lifecycle](../../EXPECTATIONS.md)
- [Auto-start narrative](auto-start.md)
- [macOS release signing](macos-release-signing.md)
