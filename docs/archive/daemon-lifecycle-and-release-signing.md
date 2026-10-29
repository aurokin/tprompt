# Daemon Lifecycle macOS Alignment and Release Signing (archived PRD)

Historical. The daemon was later removed entirely (DECISIONS §33); signing
remains and is documented in
[docs/lifecycle/macos-release-signing.md](../lifecycle/macos-release-signing.md).

## Why

The sibling project agentscan hit macOS kernel panics in the system policy path
(AppleSystemPolicy / AMFI / syspolicyd) while it evaluated fresh ad-hoc-signed,
provenance-tagged binaries. tprompt's first response, a trust gate that ran
`codesign`/`spctl` before implicit daemon auto-start, rode the same failing
path, and also contradicted the earlier lifecycle contract (no implicit
auto-start on macOS, no debug override). What actually stopped the panics was
signing: Developer ID, hardened runtime, and notarization for releases, and
self-signing for local builds.

## Track A: signing (shipped first)

Port agentscan's setup: `scripts/sign-macos-binary.sh` (Developer ID,
`--options runtime`, `--timestamp`, verify), `scripts/notarize-macos-binary.sh`
(zip, `notarytool submit --wait`, log on failure), a tag-triggered
`release.yml` (cross-compile darwin/arm64, linux/amd64, linux/arm64; temporary
keychain; sign and notarize darwin; tarballs plus SHA256SUMS), and operator
docs.

## Track B: policy alignment (later moot)

macOS implicit auto-start disabled at compile time with no override; trust
preflight only on explicit `daemon start` / `daemon run`; `TPROMPT_NO_AUTO_START=1`
and `--no-daemon-auto-start` as hard opt-outs; macOS recovery guidance pointing
at `tprompt daemon run` in a long-lived pane.

## Locked decisions then

1. No implicit auto-start on macOS, no env override.
2. Trust preflight only for explicit starts on macOS.
3. Signed Developer ID release binaries are the supported macOS install path.
4. Local macOS development uses a foreground daemon or a self-signed build.
5. Release signing covers darwin/arm64 only; Linux artifacts are unsigned.
