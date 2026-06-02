package app

// appVersion is the build-stamped release version surfaced by `tprompt
// --version`. Release builds overwrite it via
// -ldflags "-X github.com/hsadler/tprompt/internal/app.appVersion=<version>"
// (see Makefile and .github/workflows/release.yml). Unstamped dev builds
// report "dev".
var appVersion = "dev"
