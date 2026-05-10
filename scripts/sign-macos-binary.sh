#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/sign-macos-binary.sh [--identity IDENTITY] BINARY...

Signs one or more macOS Mach-O binaries with Developer ID, hardened runtime,
and a secure timestamp.

Environment:
  TPROMPT_CODESIGN_IDENTITY  Default signing identity.
  CODESIGN                   Override the codesign binary path
                             (default /usr/bin/codesign). Used by tests.

Example:
  TPROMPT_CODESIGN_IDENTITY="Developer ID Application: Hunter Sadler (79S467K965)" \
    scripts/sign-macos-binary.sh dist/tprompt-darwin-arm64/tprompt
USAGE
}

codesign_bin="${CODESIGN:-/usr/bin/codesign}"
identity="${TPROMPT_CODESIGN_IDENTITY:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --identity)
      if [[ $# -lt 2 ]]; then
        echo "error: --identity requires a value" >&2
        exit 2
      fi
      if [[ -z "$2" ]]; then
        echo "error: --identity requires a non-empty value" >&2
        exit 2
      fi
      identity="$2"
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

if [[ -z "$identity" ]]; then
  echo "error: signing identity is required via --identity or TPROMPT_CODESIGN_IDENTITY" >&2
  exit 2
fi

if [[ $# -eq 0 ]]; then
  echo "error: at least one binary path is required" >&2
  usage >&2
  exit 2
fi

for binary in "$@"; do
  if [[ ! -f "$binary" ]]; then
    echo "error: binary not found: $binary" >&2
    exit 2
  fi

  echo "signing $binary"
  "$codesign_bin" \
    --force \
    --sign "$identity" \
    --options runtime \
    --timestamp \
    "$binary"

  "$codesign_bin" --verify --strict --verbose=4 "$binary"
  # Diagnostic-only: dump the signed code's identity/timestamp/runtime
  # fields. grep-failure must NOT fail the sign step (Apple's `codesign
  # -dv` output keys have drifted across macOS versions), so swallow a
  # zero-match exit. set -o pipefail would otherwise propagate it.
  "$codesign_bin" -dv --verbose=4 "$binary" 2>&1 \
    | /usr/bin/grep -E 'Identifier|Authority|TeamIdentifier|flags|Timestamp|Runtime|CDHash' \
    || true
done
