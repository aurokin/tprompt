#!/usr/bin/env bash
# Hand-rolled bash test harness for scripts/sign-macos-binary.sh and
# scripts/notarize-macos-binary.sh. Invoke via `make test-scripts`.
#
# Coverage:
#   - argv parsing and validation, including stream (stdout vs stderr)
#     and exit-code assertions
#   - notarize end-to-end happy path and failure path through stub
#     codesign / xcrun / ditto fixtures (CODESIGN, XCRUN, DITTO env
#     overrides exist for this).
#
# What this does NOT cover: the real codesign/xcrun/ditto invocations.
# Those require macOS and a real Mach-O binary — manual smoke against
# an actual build is the rest of the coverage surface.
set -euo pipefail

# The plutil stub delegates JSON parsing to python3, so the harness
# needs python3 in PATH. Fail loudly with an actionable message rather
# than letting the heredoc invocation produce a cryptic
# `/usr/bin/env: 'python3': No such file or directory` halfway through.
if ! command -v python3 >/dev/null 2>&1; then
  echo "test harness requires python3 in PATH (used by scripts/test/fixtures/plutil-noop)" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
sign_script="$repo_root/scripts/sign-macos-binary.sh"
notarize_script="$repo_root/scripts/notarize-macos-binary.sh"
fixtures="$repo_root/scripts/test/fixtures"

# Tests mint inline stub scripts and chmod +x them. Hardened CI images
# sometimes mount $TMPDIR with `noexec`; rooting executables under the
# repo filesystem (which is guaranteed exec) avoids that landmine.
# Per-run subdir under .runtime/ so two overlapping invocations (e.g.,
# CI matrix sharing a workspace) don't yank each other's stubs out via
# their EXIT traps.
runtime_root="$repo_root/scripts/test/.runtime"
mkdir -p "$runtime_root"
# Prune orphans from killed prior runs so disk doesn't creep. The
# 2h cutoff is conservatively long: a healthy run finishes in seconds
# and removes its own subdir, so a `run-*` older than that is dead.
find "$runtime_root" -maxdepth 1 -type d -name 'run-*' -mmin +120 \
  -exec rm -rf {} + 2>/dev/null || true
runtime_dir="$(mktemp -d "$runtime_root/run-XXXXXX")"

# Track temp dirs/files so an interrupt or mid-test failure doesn't
# orphan them. Each helper that mints a temp registers it here; the
# EXIT trap nukes whatever remains.
tmp_paths=("$runtime_dir")
cleanup() {
  local p
  for p in "${tmp_paths[@]+"${tmp_paths[@]}"}"; do
    [[ -n "$p" && -e "$p" ]] && rm -rf "$p"
  done
}
trap cleanup EXIT INT TERM

mktemp_tracked_dir() {
  local d
  d="$(mktemp -d)"
  tmp_paths+=("$d")
  printf '%s' "$d"
}

mktemp_tracked_file() {
  local f
  f="$(mktemp)"
  tmp_paths+=("$f")
  printf '%s' "$f"
}

# For inline stub scripts that need to be executed. Lives on the repo
# filesystem (see runtime_dir comment above). Tracked for consistency
# with the other helpers, even though runtime_dir's recursive cleanup
# already covers it — a future refactor that drops the parent-dir push
# shouldn't silently leak files.
mktemp_exec_file() {
  local f
  f="$(mktemp "$runtime_dir/stub-XXXXXX")"
  tmp_paths+=("$f")
  printf '%s' "$f"
}

pass_count=0
fail_count=0
red=$'\033[31m'
green=$'\033[32m'
reset=$'\033[0m'

# run_case <name> <expected_exit> <expected_stdout_match> <expected_stderr_match> -- <argv>
#
# expected_*_match are POSIX ERE patterns (passed to grep -E). Pass an
# empty string to skip that assertion.
run_case() {
  local name="$1" expected_exit="$2" expected_stdout="$3" expected_stderr="$4"
  shift 4
  if [[ "${1:-}" == "--" ]]; then shift; fi

  local stdout stderr exit_code
  stdout="$(mktemp_tracked_file)"
  stderr="$(mktemp_tracked_file)"
  set +e
  "$@" >"$stdout" 2>"$stderr"
  exit_code=$?
  set -e

  local fail=0
  if [[ "$exit_code" -ne "$expected_exit" ]]; then
    fail=1
    echo "  expected exit $expected_exit, got $exit_code"
  fi
  if [[ -n "$expected_stdout" ]] && ! grep -Eq -- "$expected_stdout" "$stdout"; then
    fail=1
    echo "  expected stdout to match: $expected_stdout"
    echo "  stdout was:"
    sed 's/^/    /' "$stdout"
  fi
  if [[ -n "$expected_stderr" ]] && ! grep -Eq -- "$expected_stderr" "$stderr"; then
    fail=1
    echo "  expected stderr to match: $expected_stderr"
    echo "  stderr was:"
    sed 's/^/    /' "$stderr"
  fi

  if [[ "$fail" -eq 0 ]]; then
    echo "${green}PASS${reset} $name"
    pass_count=$((pass_count + 1))
  else
    echo "${red}FAIL${reset} $name"
    fail_count=$((fail_count + 1))
  fi

  # stdout/stderr capture files only matter for one assertion.
  # Free them here so the cleanup array doesn't grow with the suite
  # (handy if $TMPDIR is a small tmpfs).
  rm -f "$stdout" "$stderr"
}

# ============================================================
# sign-macos-binary.sh
# ============================================================

# Help on stdout, exit 0.
run_case "sign: -h prints usage on stdout" 0 'Usage: scripts/sign-macos-binary.sh' '' \
  -- "$sign_script" -h
run_case "sign: --help prints usage on stdout" 0 'Usage: scripts/sign-macos-binary.sh' '' \
  -- "$sign_script" --help

# Errors on stderr, exit 2.
run_case "sign: missing identity → error on stderr" 2 '' 'signing identity is required' \
  -- env -u TPROMPT_CODESIGN_IDENTITY "$sign_script" /tmp/whatever
run_case "sign: --identity without value → error" 2 '' '^error: --identity requires a value$' \
  -- "$sign_script" --identity
run_case "sign: --identity empty string → error" 2 '' '^error: --identity requires a non-empty value$' \
  -- "$sign_script" --identity '' /tmp/whatever
run_case "sign: unknown flag → error" 2 '' 'unknown option' \
  -- "$sign_script" --bogus
run_case "sign: bare dash → unknown option" 2 '' 'unknown option -' \
  -- "$sign_script" -
run_case "sign: identity present, no binary → error" 2 '' 'at least one binary path is required' \
  -- "$sign_script" --identity 'X'
run_case "sign: nonexistent binary → error" 2 '' 'binary not found' \
  -- "$sign_script" --identity 'X' /this/does/not/exist

# Happy path with stub codesign — assert it ran and emitted the
# diagnostic line filtered by the grep at the bottom of the script.
sign_workdir="$(mktemp_tracked_dir)"
sign_target="$sign_workdir/tprompt"
: > "$sign_target"
run_case "sign: stub codesign accepts and prints diagnostic" 0 'Identifier' '' \
  -- env CODESIGN="$fixtures/codesign-noop" "$sign_script" --identity 'X' "$sign_target"

# Multiple --identity flags: last wins. The codesign stub doesn't
# currently record args, so verify indirectly: the script must reach
# the codesign invocation (exit 0) without erroring on either parse.
multi_id_workdir="$(mktemp_tracked_dir)"
multi_target="$multi_id_workdir/tprompt"
: > "$multi_target"
run_case "sign: multiple --identity flags resolve last-wins" 0 'signing' '' \
  -- env CODESIGN="$fixtures/codesign-noop" \
  "$sign_script" --identity 'first' --identity 'second' "$multi_target"

# Identity from env var (no --identity flag).
env_id_workdir="$(mktemp_tracked_dir)"
env_id_target="$env_id_workdir/tprompt"
: > "$env_id_target"
run_case "sign: identity from TPROMPT_CODESIGN_IDENTITY env" 0 'signing' '' \
  -- env CODESIGN="$fixtures/codesign-noop" TPROMPT_CODESIGN_IDENTITY='X' \
  "$sign_script" "$env_id_target"

# ============================================================
# notarize-macos-binary.sh
# ============================================================

# Help on stdout, exit 0.
run_case "notarize: -h prints usage on stdout" 0 'Usage: scripts/notarize-macos-binary.sh' '' \
  -- "$notarize_script" -h

# Argv error paths.
run_case "notarize: zero args → error" 2 '' 'exactly one binary path is required' \
  -- env -u TPROMPT_APPLE_TEAM_ID "$notarize_script"
run_case "notarize: more than one binary arg → error" 2 '' 'exactly one binary path is required' \
  -- env -u TPROMPT_APPLE_TEAM_ID "$notarize_script" /a /b
run_case "notarize: --team-id without value → error" 2 '' '^error: --team-id requires a value$' \
  -- "$notarize_script" --team-id
run_case "notarize: --team-id empty string → error" 2 '' '^error: --team-id requires a non-empty value$' \
  -- "$notarize_script" --team-id '' /a
run_case "notarize: --profile without value → error" 2 '' '^error: --profile requires a value$' \
  -- "$notarize_script" --profile
run_case "notarize: --profile empty string → error" 2 '' '^error: --profile requires a non-empty value$' \
  -- "$notarize_script" --profile '' --team-id T /a
run_case "notarize: --keychain without value → error" 2 '' '^error: --keychain requires a value$' \
  -- "$notarize_script" --keychain
run_case "notarize: --keychain empty string → error" 2 '' '^error: --keychain requires a non-empty value$' \
  -- "$notarize_script" --keychain '' --team-id T /a
run_case "notarize: --profile set, no team-id → error" 2 '' 'team ID is required' \
  -- env -u TPROMPT_APPLE_TEAM_ID "$notarize_script" --profile p /a
run_case "notarize: unknown flag → error" 2 '' 'unknown option' \
  -- "$notarize_script" --bogus
run_case "notarize: bare dash → unknown option" 2 '' 'unknown option -' \
  -- "$notarize_script" -
run_case "notarize: nonexistent binary → error" 2 '' 'binary not found' \
  -- "$notarize_script" --team-id 'T' /this/does/not/exist

# Happy path: status=Accepted via stub xcrun.
accepted_workdir="$(mktemp_tracked_dir)"
accepted_target="$accepted_workdir/tprompt"
: > "$accepted_target"
run_case "notarize: stub xcrun returns Accepted → exit 0" 0 'notarization accepted: abc-123-def' '' \
  -- env CODESIGN="$fixtures/codesign-noop" \
       DITTO="$fixtures/ditto-noop" \
       PLUTIL="$fixtures/plutil-noop" \
       XCRUN="$fixtures/xcrun-accepted" \
  "$notarize_script" --team-id 'TEST' "$accepted_target"

# Failure path: status=Invalid → exit 1, log fetched and printed to stderr.
invalid_workdir="$(mktemp_tracked_dir)"
invalid_target="$invalid_workdir/tprompt"
: > "$invalid_target"
run_case "notarize: stub xcrun returns Invalid → exit 1, log printed" 1 '' 'Archive contents not signed' \
  -- env CODESIGN="$fixtures/codesign-noop" \
       DITTO="$fixtures/ditto-noop" \
       PLUTIL="$fixtures/plutil-noop" \
       XCRUN="$fixtures/xcrun-invalid" \
  "$notarize_script" --team-id 'TEST' "$invalid_target"

# Failure path WITH --keychain set: same Invalid stub, plus --keychain.
# Catches a regression where reordering log_args misroutes the log
# output path. The stubs now parse argv explicitly so the log content
# still lands in $workdir/notary-log.json and gets cat'd to stderr.
keychain_invalid_workdir="$(mktemp_tracked_dir)"
keychain_invalid_target="$keychain_invalid_workdir/tprompt"
: > "$keychain_invalid_target"
run_case "notarize: --keychain + Invalid stub still surfaces log content" 1 '' 'Archive contents not signed' \
  -- env CODESIGN="$fixtures/codesign-noop" \
       DITTO="$fixtures/ditto-noop" \
       PLUTIL="$fixtures/plutil-noop" \
       XCRUN="$fixtures/xcrun-invalid" \
  "$notarize_script" --team-id 'TEST' --keychain '/tmp/fake.keychain-db' "$keychain_invalid_target"

# Team id from env (no flag).
env_team_workdir="$(mktemp_tracked_dir)"
env_team_target="$env_team_workdir/tprompt"
: > "$env_team_target"
run_case "notarize: team id from TPROMPT_APPLE_TEAM_ID env" 0 'notarization accepted' '' \
  -- env CODESIGN="$fixtures/codesign-noop" \
       DITTO="$fixtures/ditto-noop" \
       PLUTIL="$fixtures/plutil-noop" \
       XCRUN="$fixtures/xcrun-accepted" \
       TPROMPT_APPLE_TEAM_ID='TEST' \
  "$notarize_script" "$env_team_target"

# Status-parser: extraction must be structure-aware (not substring).
# Construct a fixture-shaped JSON where `"jobId"` appears before `"id"`
# and `"message"` contains a literal `"status": "junk"`. A naïve
# substring grep would mis-extract; the production parser uses plutil's
# JSON keypath extraction so it picks the top-level `id`/`status`.
custom_xcrun="$(mktemp_exec_file)"
cat >"$custom_xcrun" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  notarytool)
    case "${2:-}" in
      submit)
        # Note: nested `"status": "Invalid"` appears BEFORE the
        # top-level `"status": "Accepted"` in source order, so a
        # substring parser would extract "Invalid" first and bail to
        # the failure path. plutil's keypath extraction must pick the
        # top-level scalar regardless of order.
        cat <<'JSON'
{
  "jobId": "wrong-substring-id",
  "id": "real-id",
  "message": "diagnostic mentioning \"status\": \"Invalid\" inline",
  "status": "Accepted"
}
JSON
        exit 0
        ;;
    esac
    ;;
esac
exit 1
STUB
chmod +x "$custom_xcrun"
status_parse_workdir="$(mktemp_tracked_dir)"
status_parse_target="$status_parse_workdir/tprompt"
: > "$status_parse_target"
run_case "notarize: status parser ignores substring keys" 0 'notarization accepted: real-id' '' \
  -- env CODESIGN="$fixtures/codesign-noop" \
       DITTO="$fixtures/ditto-noop" \
       PLUTIL="$fixtures/plutil-noop" \
       XCRUN="$custom_xcrun" \
  "$notarize_script" --team-id 'TEST' "$status_parse_target"

# Compact (single-line) JSON must parse successfully. Apple's
# `--output-format json` only guarantees JSON, not pretty-printed
# JSON, so a future Xcode emitting compact output must not break the
# happy path. plutil parses both compact and pretty JSON the same way.
single_line_xcrun="$(mktemp_exec_file)"
cat >"$single_line_xcrun" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  notarytool)
    case "${2:-}" in
      submit)
        printf '{"id":"single-line-id","status":"Accepted"}\n'
        exit 0
        ;;
    esac
    ;;
esac
exit 1
STUB
chmod +x "$single_line_xcrun"
single_line_workdir="$(mktemp_tracked_dir)"
single_line_target="$single_line_workdir/tprompt"
: > "$single_line_target"
run_case "notarize: compact single-line JSON parses successfully" 0 'notarization accepted: single-line-id' '' \
  -- env CODESIGN="$fixtures/codesign-noop" \
       DITTO="$fixtures/ditto-noop" \
       PLUTIL="$fixtures/plutil-noop" \
       XCRUN="$single_line_xcrun" \
  "$notarize_script" --team-id 'TEST' "$single_line_target"

# Empty submission output (xcrun crashed pre-bind / format change):
# parser must report a parseable error rather than silently fall into
# the `status != "Accepted"` branch with an empty submission_id.
empty_xcrun="$(mktemp_exec_file)"
cat >"$empty_xcrun" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$empty_xcrun"
empty_status_workdir="$(mktemp_tracked_dir)"
empty_status_target="$empty_status_workdir/tprompt"
: > "$empty_status_target"
run_case "notarize: empty submission JSON → diagnostic + exit 1" 1 '' 'could not parse notary status' \
  -- env CODESIGN="$fixtures/codesign-noop" \
       DITTO="$fixtures/ditto-noop" \
       PLUTIL="$fixtures/plutil-noop" \
       XCRUN="$empty_xcrun" \
  "$notarize_script" --team-id 'TEST' "$empty_status_target"

# xcrun submit failure: production now captures PIPESTATUS and emits a
# specific diagnostic instead of letting `set -e -o pipefail` abort
# silently with a bare bash trace.
failing_xcrun="$(mktemp_exec_file)"
cat >"$failing_xcrun" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  notarytool)
    case "${2:-}" in
      submit)
        echo "xcrun: simulated network failure" >&2
        exit 7
        ;;
    esac
    ;;
esac
exit 1
STUB
chmod +x "$failing_xcrun"
failing_xcrun_workdir="$(mktemp_tracked_dir)"
failing_xcrun_target="$failing_xcrun_workdir/tprompt"
: > "$failing_xcrun_target"
run_case "notarize: xcrun submit non-zero → diagnostic + exit 1" 1 '' 'xcrun notarytool submit failed' \
  -- env CODESIGN="$fixtures/codesign-noop" \
       DITTO="$fixtures/ditto-noop" \
       PLUTIL="$fixtures/plutil-noop" \
       XCRUN="$failing_xcrun" \
  "$notarize_script" --team-id 'TEST' "$failing_xcrun_target"

echo
echo "$pass_count passed, $fail_count failed"
[[ "$fail_count" -eq 0 ]]
