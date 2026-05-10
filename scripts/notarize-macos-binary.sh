#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/notarize-macos-binary.sh [--profile PROFILE] [--team-id TEAM_ID] BINARY

Submits a signed macOS CLI binary to Apple's notary service by wrapping it in a
temporary zip. Bare CLI binaries and zip archives cannot be stapled; the
notarization ticket is associated with the signed code hash.

Environment:
  TPROMPT_NOTARY_PROFILE   Keychain profile name. Default: tprompt-notary
  TPROMPT_NOTARY_KEYCHAIN  Optional keychain path for the stored profile.
  TPROMPT_APPLE_TEAM_ID    Apple Developer Team ID.
  CODESIGN                 Override the codesign binary path
                           (default /usr/bin/codesign). Used by tests.
  XCRUN                    Override the xcrun binary path
                           (default /usr/bin/xcrun). Used by tests.
  DITTO                    Override the ditto binary path
                           (default /usr/bin/ditto). Used by tests.

Example:
  TPROMPT_APPLE_TEAM_ID=79S467K965 \
    scripts/notarize-macos-binary.sh dist/tprompt-darwin-arm64/tprompt
USAGE
}

codesign_bin="${CODESIGN:-/usr/bin/codesign}"
xcrun_bin="${XCRUN:-/usr/bin/xcrun}"
ditto_bin="${DITTO:-/usr/bin/ditto}"

profile="${TPROMPT_NOTARY_PROFILE:-tprompt-notary}"
keychain="${TPROMPT_NOTARY_KEYCHAIN:-}"
team_id="${TPROMPT_APPLE_TEAM_ID:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)
      if [[ $# -lt 2 ]]; then
        echo "error: --profile requires a value" >&2
        exit 2
      fi
      if [[ -z "$2" ]]; then
        echo "error: --profile requires a non-empty value" >&2
        exit 2
      fi
      profile="$2"
      shift 2
      ;;
    --team-id)
      if [[ $# -lt 2 ]]; then
        echo "error: --team-id requires a value" >&2
        exit 2
      fi
      if [[ -z "$2" ]]; then
        echo "error: --team-id requires a non-empty value" >&2
        exit 2
      fi
      team_id="$2"
      shift 2
      ;;
    --keychain)
      if [[ $# -lt 2 ]]; then
        echo "error: --keychain requires a value" >&2
        exit 2
      fi
      if [[ -z "$2" ]]; then
        echo "error: --keychain requires a non-empty value" >&2
        exit 2
      fi
      keychain="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      echo "error: unknown option $1" >&2
      usage >&2
      exit 2
      ;;
    *)
      break
      ;;
  esac
done

if [[ $# -ne 1 ]]; then
  echo "error: exactly one binary path is required" >&2
  usage >&2
  exit 2
fi

if [[ -z "$team_id" ]]; then
  echo "error: team ID is required via --team-id or TPROMPT_APPLE_TEAM_ID" >&2
  exit 2
fi

binary="$1"
if [[ ! -f "$binary" ]]; then
  echo "error: binary not found: $binary" >&2
  exit 2
fi

"$codesign_bin" --verify --strict --verbose=4 "$binary"

workdir="$(/usr/bin/mktemp -d "${TMPDIR:-/tmp}/tprompt-notary.XXXXXX")"
# Defensive: only nuke a real path. set -u + a future refactor that
# forgets to assign workdir would otherwise turn this into `rm -rf ""`.
trap '[[ -n "${workdir:-}" && -d "$workdir" ]] && rm -rf "$workdir"' EXIT

archive="$workdir/$(/usr/bin/basename "$binary").zip"
"$ditto_bin" -c -k --keepParent "$binary" "$archive"

echo "submitting $archive"
submission_json="$workdir/submission.json"
# notarytool submit syntax: `submit <archive> {auth options} [other options]`.
# Group auth flags first, then everything else, so a runtime grammar
# tightening on Apple's side doesn't break the submit step.
submit_args=(
  notarytool submit "$archive"
  --keychain-profile "$profile"
  --team-id "$team_id"
)
if [[ -n "$keychain" ]]; then
  submit_args+=(--keychain "$keychain")
fi
submit_args+=(
  --wait
  --output-format json
)

# Capture xcrun's exit explicitly so we can emit a real diagnostic
# instead of a bare bash trace from `set -e -o pipefail`.
set +e
"$xcrun_bin" "${submit_args[@]}" | /usr/bin/tee "$submission_json"
xcrun_rc=${PIPESTATUS[0]}
set -e
if [[ "$xcrun_rc" -ne 0 ]]; then
  echo "error: xcrun notarytool submit failed (rc=$xcrun_rc); see $submission_json" >&2
  exit 1
fi

# Anchor the JSON-key match to start-of-line + optional indent so
# `"id"` doesn't substring-match `"jobId"` / `"appleId"` / etc., and
# `"status"` doesn't pick up a nested message string. Apple's
# `--output-format json` is pretty-printed in practice, so each key
# starts a line.
status="$(/usr/bin/sed -n 's/^[[:space:]]*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$submission_json" | /usr/bin/head -n 1)"
submission_id="$(/usr/bin/sed -n 's/^[[:space:]]*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$submission_json" | /usr/bin/head -n 1)"

if [[ -z "$status" ]]; then
  echo "error: could not parse notary status from $submission_json" >&2
  /bin/cat "$submission_json" >&2 || true
  exit 1
fi

if [[ "$status" != "Accepted" ]]; then
  if [[ -n "$submission_id" ]]; then
    echo "notarization failed; fetching log for $submission_id" >&2
    # notarytool log syntax is: `log <id> {auth options} [output-path]`.
    # --keychain is an auth option, so it must precede the positional
    # output path. Building log_args in two halves keeps the positional
    # last regardless of whether --keychain is set.
    log_args=(
      notarytool log "$submission_id"
      --keychain-profile "$profile"
      --team-id "$team_id"
    )
    if [[ -n "$keychain" ]]; then
      log_args+=(--keychain "$keychain")
    fi
    log_args+=("$workdir/notary-log.json")
    "$xcrun_bin" "${log_args[@]}" || true
    [[ -f "$workdir/notary-log.json" ]] && /bin/cat "$workdir/notary-log.json" >&2
  fi
  exit 1
fi

echo "notarization accepted: $submission_id"
