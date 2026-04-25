package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// ---------------------------------------------------------------------------
// Default() tests (unchanged from Phase 1)
// ---------------------------------------------------------------------------

func TestDefaultMatchesDocumentedMVPValues(t *testing.T) {
	got := Default()

	if got.DefaultMode != "paste" {
		t.Fatalf("Default().DefaultMode = %q, want %q", got.DefaultMode, "paste")
	}
	if got.DefaultEnter {
		t.Fatal("Default().DefaultEnter = true, want false")
	}
	if got.SocketPath != "~/.local/state/tprompt/daemon.sock" {
		t.Fatalf("Default().SocketPath = %q", got.SocketPath)
	}
	if got.LogPath != "~/.local/state/tprompt/daemon.log" {
		t.Fatalf("Default().LogPath = %q", got.LogPath)
	}
	if got.PickerCommand != "fzf" {
		t.Fatalf("Default().PickerCommand = %q, want %q", got.PickerCommand, "fzf")
	}
	if got.VerificationTimeoutMS != 5000 {
		t.Fatalf("Default().VerificationTimeoutMS = %d, want %d", got.VerificationTimeoutMS, 5000)
	}
	if got.VerificationPollIntervalMS != 100 {
		t.Fatalf("Default().VerificationPollIntervalMS = %d, want %d", got.VerificationPollIntervalMS, 100)
	}
	if got.ClipboardReadCommand != "" {
		t.Fatalf("Default().ClipboardReadCommand = %q, want empty string", got.ClipboardReadCommand)
	}
	if got.MaxPasteBytes != 1<<20 {
		t.Fatalf("Default().MaxPasteBytes = %d, want %d", got.MaxPasteBytes, 1<<20)
	}
	if got.PostInjectionVerification {
		t.Fatal("Default().PostInjectionVerification = true, want false")
	}
}

func TestDefaultSanitizeIsOff(t *testing.T) {
	if got := Default().Sanitize; got != "off" {
		t.Fatalf("Default().Sanitize = %q, want %q", got, "off")
	}
}

func TestDefaultKeybindPoolMatchesSpec(t *testing.T) {
	want := "12345qerfgtzxc"
	got := Default().KeybindPool
	if got != want {
		t.Fatalf("Default().KeybindPool = %q, want %q", got, want)
	}
}

func TestDefaultReservedKeysUseRoleToKeyMapping(t *testing.T) {
	want := map[string]string{
		"clipboard": "P",
		"search":    "/",
		"cancel":    "Esc",
		"select":    "Enter",
	}

	got := Default().ReservedKeys
	if len(got) != len(want) {
		t.Fatalf("reserved_keys len = %d, want %d", len(got), len(want))
	}
	for role, key := range want {
		if got[role] != key {
			t.Errorf("reserved_keys[%q] = %q, want %q", role, got[role], key)
		}
	}
}

func TestConfigDecodesDocumentedExampleShape(t *testing.T) {
	const configTOML = `
prompts_dir = "~/.config/tprompt/prompts"
default_mode = "paste"
default_enter = false
socket_path = "~/.local/state/tprompt/daemon.sock"
log_path = "~/.local/state/tprompt/daemon.log"
picker_command = "fzf"
verification_timeout_ms = 5000
verification_poll_interval_ms = 100
post_injection_verification = true
clipboard_read_command = ""
max_paste_bytes = 1048576
sanitize = "off"
keybind_pool = "12345qerfgtzxc"

[reserved_keys]
clipboard = "P"
search = "/"
cancel = "Esc"
select = "Enter"
`

	var got Config
	if _, err := toml.Decode(configTOML, &got); err != nil {
		t.Fatalf("toml.Decode returned error: %v", err)
	}

	if got.PromptsDir != "~/.config/tprompt/prompts" {
		t.Fatalf("PromptsDir = %q", got.PromptsDir)
	}
	if got.DefaultMode != "paste" {
		t.Fatalf("DefaultMode = %q", got.DefaultMode)
	}
	if got.PickerCommand != "fzf" {
		t.Fatalf("PickerCommand = %q", got.PickerCommand)
	}
	if !got.PostInjectionVerification {
		t.Fatal("PostInjectionVerification = false, want true")
	}
	if got.KeybindPool != "12345qerfgtzxc" {
		t.Fatalf("KeybindPool = %q", got.KeybindPool)
	}
	if got.ReservedKeys["clipboard"] != "P" || got.ReservedKeys["select"] != "Enter" {
		t.Fatalf("ReservedKeys decoded incorrectly: %#v", got.ReservedKeys)
	}
}

func TestConfigRejectsArrayWhereDocsRequireScalarString(t *testing.T) {
	const invalidTOML = `
picker_command = ["fzf"]
`

	var got Config
	_, err := toml.Decode(invalidTOML, &got)
	if err == nil {
		t.Fatal("toml.Decode unexpectedly accepted array for picker_command")
	}
	if !strings.Contains(err.Error(), "picker_command") {
		t.Fatalf("toml.Decode error = %q, want reference to picker_command", err)
	}
}

// ---------------------------------------------------------------------------
// Load tests
// ---------------------------------------------------------------------------

func TestLoadDecodesValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeTOML(t, path, `
prompts_dir = "/tmp/prompts"
default_mode = "type"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PromptsDir != "/tmp/prompts" {
		t.Fatalf("PromptsDir = %q", cfg.PromptsDir)
	}
	if cfg.DefaultMode != "type" {
		t.Fatalf("DefaultMode = %q", cfg.DefaultMode)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeTOML(t, path, `
prompts_dir = "/tmp/prompts"
bogus_key = "oops"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("want error for unknown key, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %T: %v", err, err)
	}
	if !strings.Contains(ve.Message, "bogus_key") {
		t.Fatalf("error should mention bogus_key: %v", ve)
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.toml")
	if err == nil {
		t.Fatal("want error for missing file, got nil")
	}
}

// ---------------------------------------------------------------------------
// LoadOrDefault tests
// ---------------------------------------------------------------------------

func TestLoadOrDefaultUsesExplicitPath(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)
	path := filepath.Join(dir, "config.toml")
	writeTOML(t, path, `prompts_dir = "`+promptsDir+`"`)

	cfg, resolved, err := LoadOrDefault(path, os.Getenv)
	if err != nil {
		t.Fatalf("LoadOrDefault: %v", err)
	}
	if resolved != path {
		t.Fatalf("resolved path = %q, want %q", resolved, path)
	}
	if cfg.PromptsDir != promptsDir {
		t.Fatalf("PromptsDir = %q", cfg.PromptsDir)
	}
}

func TestLoadOrDefaultFallsBackToDefaults(t *testing.T) {
	cfg, resolved, err := LoadOrDefault("", os.Getenv)
	if err != nil {
		t.Fatalf("LoadOrDefault: %v", err)
	}
	if resolved != "" {
		t.Fatalf("resolved path = %q, want empty", resolved)
	}
	if cfg.DefaultMode != "paste" {
		t.Fatalf("DefaultMode = %q, want paste", cfg.DefaultMode)
	}
}

// ---------------------------------------------------------------------------
// Normalize tests
// ---------------------------------------------------------------------------

func TestNormalizeExpandsHomePaths(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	r, err := Normalize(cfg, "")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if strings.Contains(r.SocketPath, "~") {
		t.Fatalf("SocketPath still contains ~: %q", r.SocketPath)
	}
	if strings.Contains(r.LogPath, "~") {
		t.Fatalf("LogPath still contains ~: %q", r.LogPath)
	}
}

func TestNormalizeReservedPrintableKeys(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	r, err := Normalize(cfg, "")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if role, ok := r.ReservedPrintable['p']; !ok || role != "clipboard" {
		t.Fatalf("ReservedPrintable['p'] = %q, %v; want clipboard, true", role, ok)
	}
	if role, ok := r.ReservedPrintable['/']; !ok || role != "search" {
		t.Fatalf("ReservedPrintable['/'] = %q, %v; want search, true", role, ok)
	}
}

func TestNormalizeReservedSymbolicKeys(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	r, err := Normalize(cfg, "")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if sym, ok := r.ReservedSymbolic["cancel"]; !ok || sym != "Esc" {
		t.Fatalf("ReservedSymbolic[cancel] = %q, %v; want Esc, true", sym, ok)
	}
	if sym, ok := r.ReservedSymbolic["select"]; !ok || sym != "Enter" {
		t.Fatalf("ReservedSymbolic[select] = %q, %v; want Enter, true", sym, ok)
	}
}

func TestNormalizeDisabledReservedKey(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	cfg.ReservedKeys["clipboard"] = ""
	r, err := Normalize(cfg, "")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if _, ok := r.ReservedPrintable['p']; ok {
		t.Fatal("disabled clipboard key should not appear in ReservedPrintable")
	}
}

func TestNormalizePoolRemovesReservedKeys(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	cfg.ReservedKeys = map[string]string{"action": "1"}
	r, err := Normalize(cfg, "")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	for _, ch := range r.KeybindPool {
		if ch == '1' {
			t.Fatal("pool should not contain reserved key '1'")
		}
	}
}

func TestNormalizePoolDeduplicates(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	cfg.KeybindPool = "aabbcc"
	cfg.ReservedKeys = map[string]string{}
	r, err := Normalize(cfg, "")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(r.KeybindPool) != 3 {
		t.Fatalf("pool len = %d, want 3", len(r.KeybindPool))
	}
}

func TestNormalizeRejectsInvalidReservedKey(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	cfg.ReservedKeys["clipboard"] = "ctrl+x"
	_, err := Normalize(cfg, "")
	if err == nil {
		t.Fatal("want error for multi-char reserved key, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "reserved_keys.clipboard" {
		t.Fatalf("Field = %q", ve.Field)
	}
}

func TestNormalizeClipboardArgv(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	cfg.ClipboardReadCommand = `pbpaste -pboard general`
	r, err := Normalize(cfg, "")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(r.ClipboardArgv) != 3 || r.ClipboardArgv[0] != "pbpaste" {
		t.Fatalf("ClipboardArgv = %v", r.ClipboardArgv)
	}
}

func TestNormalizeRejectsUnparseableClipboardCommand(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	cfg.ClipboardReadCommand = `"unterminated`
	_, err := Normalize(cfg, "")
	if err == nil {
		t.Fatal("want error for bad clipboard_read_command, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "clipboard_read_command" {
		t.Fatalf("Field = %q", ve.Field)
	}
}

func TestNormalizePickerArgv(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	cfg.PickerCommand = `fzf --prompt "tprompt> "`
	r, err := Normalize(cfg, "")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	want := []string{"fzf", "--prompt", "tprompt> "}
	if !stringSliceEqual(r.PickerArgv, want) {
		t.Fatalf("PickerArgv = %v, want %v", r.PickerArgv, want)
	}
}

func TestNormalizeRejectsUnparseablePickerCommand(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	cfg.PickerCommand = `"unterminated`
	_, err := Normalize(cfg, "")
	if err == nil {
		t.Fatal("want error for bad picker_command, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "picker_command" {
		t.Fatalf("Field = %q", ve.Field)
	}
}

func TestNormalizeEmptyClipboardCommand(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)

	cfg := Default()
	cfg.PromptsDir = promptsDir
	r, err := Normalize(cfg, "")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if r.ClipboardArgv != nil {
		t.Fatalf("ClipboardArgv = %v, want nil", r.ClipboardArgv)
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestResolveDaemonIgnoresPromptAndClipboardFields(t *testing.T) {
	cfg := Default()
	cfg.PromptsDir = ""
	cfg.ClipboardReadCommand = `"unterminated`
	cfg.ReservedKeys["clipboard"] = "ctrl+x"
	cfg.PostInjectionVerification = true

	r := ResolveDaemon(cfg, "/tmp/config.toml")
	if r.SocketPath == "" {
		t.Fatal("ResolveDaemon left SocketPath empty")
	}
	if r.LogPath == "" {
		t.Fatal("ResolveDaemon left LogPath empty")
	}
	if r.ConfigPath != "/tmp/config.toml" {
		t.Fatalf("ConfigPath = %q, want %q", r.ConfigPath, "/tmp/config.toml")
	}
	if !r.PostInjectionVerification {
		t.Fatal("ResolveDaemon did not carry PostInjectionVerification")
	}
}

// ---------------------------------------------------------------------------
// Validate tests
// ---------------------------------------------------------------------------

func TestValidateAcceptsValidResolved(t *testing.T) {
	r := validResolved(t)
	if err := Validate(r); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsEmptyPromptsDir(t *testing.T) {
	r := validResolved(t)
	r.PromptsDir = ""
	assertValidationField(t, Validate(r), "prompts_dir")
}

func TestValidateRejectsInvalidMode(t *testing.T) {
	r := validResolved(t)
	r.DefaultMode = "yolo"
	assertValidationField(t, Validate(r), "default_mode")
}

func TestValidateRejectsInvalidSanitize(t *testing.T) {
	r := validResolved(t)
	r.Sanitize = "maybe"
	assertValidationField(t, Validate(r), "sanitize")
}

func TestValidateRejectsEmptySocketPath(t *testing.T) {
	r := validResolved(t)
	r.SocketPath = ""
	assertValidationField(t, Validate(r), "socket_path")
}

func TestValidateRejectsNonPositiveMaxPasteBytes(t *testing.T) {
	r := validResolved(t)
	r.MaxPasteBytes = 0
	assertValidationField(t, Validate(r), "max_paste_bytes")
}

func TestValidatePasteAcceptsEmptyPromptsDir(t *testing.T) {
	r := validResolved(t)
	r.PromptsDir = ""
	if err := ValidatePaste(r); err != nil {
		t.Fatalf("ValidatePaste: %v", err)
	}
}

func TestValidatePasteRejectsDeliveryConfigErrors(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(*Resolved)
		field string
	}{
		{
			name:  "invalid mode",
			mut:   func(r *Resolved) { r.DefaultMode = "yolo" },
			field: "default_mode",
		},
		{
			name:  "invalid sanitize",
			mut:   func(r *Resolved) { r.Sanitize = "maybe" },
			field: "sanitize",
		},
		{
			name:  "non-positive max paste bytes",
			mut:   func(r *Resolved) { r.MaxPasteBytes = 0 },
			field: "max_paste_bytes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validResolved(t)
			r.PromptsDir = ""
			tc.mut(&r)
			assertValidationField(t, ValidatePaste(r), tc.field)
		})
	}
}

// ---------------------------------------------------------------------------
// parseReservedKey tests
// ---------------------------------------------------------------------------

func TestParseReservedKeyPrintable(t *testing.T) {
	rk, err := parseReservedKey("P")
	if err != nil {
		t.Fatalf("parseReservedKey: %v", err)
	}
	if rk.Printable != 'P' || rk.Symbolic != "" || rk.Disabled {
		t.Fatalf("got %+v", rk)
	}
}

func TestParseReservedKeySymbolic(t *testing.T) {
	for _, tc := range []struct {
		input, want string
	}{
		{"Esc", "Esc"},
		{"esc", "Esc"},
		{"ESC", "Esc"},
		{"Enter", "Enter"},
		{"enter", "Enter"},
		{"Tab", "Tab"},
		{"Space", "Space"},
	} {
		rk, err := parseReservedKey(tc.input)
		if err != nil {
			t.Fatalf("parseReservedKey(%q): %v", tc.input, err)
		}
		if rk.Symbolic != tc.want {
			t.Fatalf("parseReservedKey(%q).Symbolic = %q, want %q", tc.input, rk.Symbolic, tc.want)
		}
	}
}

func TestParseReservedKeyDisabled(t *testing.T) {
	rk, err := parseReservedKey("")
	if err != nil {
		t.Fatalf("parseReservedKey: %v", err)
	}
	if !rk.Disabled {
		t.Fatal("want Disabled=true")
	}
}

func TestParseReservedKeyRejectsMultiChar(t *testing.T) {
	_, err := parseReservedKey("ctrl+x")
	if err == nil {
		t.Fatal("want error for multi-char, got nil")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func validResolved(t *testing.T) Resolved {
	t.Helper()
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")
	mustMkdir(t, promptsDir)
	return Resolved{
		PromptsDir:    promptsDir,
		DefaultMode:   "paste",
		Sanitize:      "off",
		SocketPath:    filepath.Join(dir, "daemon.sock"),
		LogPath:       filepath.Join(dir, "daemon.log"),
		MaxPasteBytes: 1 << 20,
	}
}

func assertValidationField(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want validation error for %s, got nil", field)
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != field {
		t.Fatalf("Field = %q, want %q", ve.Field, field)
	}
}

func writeTOML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
