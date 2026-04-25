// Package config loads and validates tprompt's TOML configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/google/shlex"
)

// Config mirrors the user-facing fields documented in docs/storage/config.md.
type Config struct {
	PromptsDir                 string            `toml:"prompts_dir"`
	DefaultMode                string            `toml:"default_mode"`
	DefaultEnter               bool              `toml:"default_enter"`
	SocketPath                 string            `toml:"socket_path"`
	LogPath                    string            `toml:"log_path"`
	DaemonAutoStart            bool              `toml:"daemon_auto_start"`
	PickerCommand              string            `toml:"picker_command"`
	VerificationTimeoutMS      int               `toml:"verification_timeout_ms"`
	VerificationPollIntervalMS int               `toml:"verification_poll_interval_ms"`
	ClipboardReadCommand       string            `toml:"clipboard_read_command"`
	MaxPasteBytes              int64             `toml:"max_paste_bytes"`
	Sanitize                   string            `toml:"sanitize"`
	KeybindPool                string            `toml:"keybind_pool"`
	ReservedKeys               map[string]string `toml:"reserved_keys"`
}

// Default returns a Config populated with the MVP defaults.
func Default() Config {
	return Config{
		DefaultMode:                "paste",
		DefaultEnter:               false,
		SocketPath:                 "~/.local/state/tprompt/daemon.sock",
		LogPath:                    "~/.local/state/tprompt/daemon.log",
		DaemonAutoStart:            false,
		PickerCommand:              "fzf",
		VerificationTimeoutMS:      5000,
		VerificationPollIntervalMS: 100,
		ClipboardReadCommand:       "",
		MaxPasteBytes:              1 << 20,
		Sanitize:                   "off",
		KeybindPool:                "12345qerfgtzxc",
		ReservedKeys: map[string]string{
			"clipboard": "P",
			"search":    "/",
			"cancel":    "Esc",
			"select":    "Enter",
		},
	}
}

// ReservedKey is a parsed reserved-key value: either a printable rune, a
// symbolic name (Esc/Enter/Tab/Space), or disabled (empty string in config).
type ReservedKey struct {
	Printable rune
	Symbolic  string
	Disabled  bool
}

// Resolved is the normalized, validated form of Config ready for use by the
// rest of the application. Produced by Normalize after Load/LoadOrDefault.
type Resolved struct {
	PromptsDir                 string
	DefaultMode                string
	DefaultEnter               bool
	SocketPath                 string
	LogPath                    string
	DaemonAutoStart            bool
	PickerCommand              string
	VerificationTimeoutMS      int
	VerificationPollIntervalMS int
	ClipboardReadCommand       string
	ClipboardArgv              []string
	PickerArgv                 []string
	MaxPasteBytes              int64
	Sanitize                   string
	KeybindPool                []rune
	ReservedPrintable          map[rune]string
	ReservedSymbolic           map[string]string
	ConfigPath                 string
}

// ResolveDaemon extracts only the config fields the daemon lifecycle needs.
// Unlike Normalize, it intentionally skips prompt-store, keybinding, and
// clipboard parsing so daemon start/status are not coupled to unrelated
// validation.
func ResolveDaemon(cfg Config, configPath string) Resolved {
	return Resolved{
		SocketPath:      expandHome(cfg.SocketPath),
		LogPath:         expandHome(cfg.LogPath),
		DaemonAutoStart: cfg.DaemonAutoStart,
		MaxPasteBytes:   cfg.MaxPasteBytes,
		ConfigPath:      configPath,
	}
}

// ValidationError reports a single config validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config: %s: %s", e.Field, e.Message)
}

// symbolicKeys maps accepted symbolic key names (case-insensitive input) to
// their canonical form.
var symbolicKeys = map[string]string{
	"esc":   "Esc",
	"enter": "Enter",
	"tab":   "Tab",
	"space": "Space",
}

// Load decodes a TOML config file at path, overlaying onto Default() so
// omitted fields keep their built-in defaults. Unknown keys are rejected.
func Load(path string) (Config, error) {
	cfg := Default()
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("load config %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Config{}, &ValidationError{
			Field:   undecoded[0].String(),
			Message: fmt.Sprintf("unknown config key %q in %s", undecoded[0].String(), path),
		}
	}
	return cfg, nil
}

// LoadOrDefault loads config from the explicit path, falls back to standard
// locations, then to Default(). getenv is used to resolve XDG_CONFIG_HOME.
// Returns the resolved path ("" if using defaults) and the raw Config.
func LoadOrDefault(explicitPath string, getenv func(string) string) (Config, string, error) {
	if explicitPath != "" {
		cfg, err := Load(explicitPath)
		return cfg, explicitPath, err
	}

	candidates := standardConfigPaths(getenv)
	for _, path := range candidates {
		expanded := expandHome(path)
		if _, err := os.Stat(expanded); err != nil {
			continue
		}
		cfg, err := Load(expanded)
		return cfg, expanded, err
	}

	return Default(), "", nil
}

func standardConfigPaths(getenv func(string) string) []string {
	var paths []string
	if xdg := getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "tprompt", "config.toml"))
	}
	paths = append(paths, "~/.config/tprompt/config.toml")
	return paths
}

// Normalize converts a raw Config into a Resolved config, expanding paths,
// parsing reserved keys, normalizing the keybind pool, and parsing
// clipboard_read_command. configPath is recorded for diagnostics.
func Normalize(cfg Config, configPath string) (Resolved, error) {
	r := Resolved{
		DefaultMode:                cfg.DefaultMode,
		DefaultEnter:               cfg.DefaultEnter,
		PickerCommand:              cfg.PickerCommand,
		VerificationTimeoutMS:      cfg.VerificationTimeoutMS,
		VerificationPollIntervalMS: cfg.VerificationPollIntervalMS,
		DaemonAutoStart:            cfg.DaemonAutoStart,
		ClipboardReadCommand:       cfg.ClipboardReadCommand,
		MaxPasteBytes:              cfg.MaxPasteBytes,
		Sanitize:                   cfg.Sanitize,
		ConfigPath:                 configPath,
	}

	r.PromptsDir = expandHome(cfg.PromptsDir)
	r.SocketPath = expandHome(cfg.SocketPath)
	r.LogPath = expandHome(cfg.LogPath)

	pickerArgv, err := parseCommandArgv("picker_command", cfg.PickerCommand)
	if err != nil {
		return Resolved{}, err
	}
	r.PickerArgv = pickerArgv

	reservedPrintable := make(map[rune]string)
	reservedSymbolic := make(map[string]string)
	for role, raw := range cfg.ReservedKeys {
		rk, err := parseReservedKey(raw)
		if err != nil {
			return Resolved{}, &ValidationError{
				Field:   fmt.Sprintf("reserved_keys.%s", role),
				Message: err.Error(),
			}
		}
		if rk.Disabled {
			continue
		}
		if rk.Symbolic != "" {
			reservedSymbolic[role] = rk.Symbolic
		} else {
			reservedPrintable[unicode.ToLower(rk.Printable)] = role
		}
	}
	r.ReservedPrintable = reservedPrintable
	r.ReservedSymbolic = reservedSymbolic

	r.KeybindPool = normalizePool(cfg.KeybindPool, reservedPrintable)

	clipboardArgv, err := parseCommandArgv("clipboard_read_command", cfg.ClipboardReadCommand)
	if err != nil {
		return Resolved{}, err
	}
	r.ClipboardArgv = clipboardArgv

	return r, nil
}

func parseCommandArgv(field, command string) ([]string, error) {
	if command == "" {
		return nil, nil
	}
	argv, err := shlex.Split(command)
	if err != nil {
		return nil, &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("cannot parse as argv: %v", err),
		}
	}
	if len(argv) == 0 {
		return nil, &ValidationError{
			Field:   field,
			Message: "parses to empty argv",
		}
	}
	return argv, nil
}

// Validate checks a Resolved config for semantic errors. Call after Normalize.
func Validate(r Resolved) error {
	if r.PromptsDir == "" {
		return &ValidationError{Field: "prompts_dir", Message: "must be set"}
	}
	if err := validateDeliveryConfig(r); err != nil {
		return err
	}

	if r.SocketPath == "" {
		return &ValidationError{Field: "socket_path", Message: "must be set"}
	}

	return nil
}

// ValidatePaste checks the subset of config needed by standalone clipboard
// delivery. It intentionally does not require prompts_dir because `tprompt
// paste` never opens the prompt store.
func ValidatePaste(r Resolved) error {
	return validateDeliveryConfig(r)
}

func validateDeliveryConfig(r Resolved) error {
	switch r.DefaultMode {
	case "paste", "type":
	default:
		return &ValidationError{
			Field:   "default_mode",
			Message: fmt.Sprintf("invalid value %q: must be paste or type", r.DefaultMode),
		}
	}

	switch r.Sanitize {
	case "off", "safe", "strict":
	default:
		return &ValidationError{
			Field:   "sanitize",
			Message: fmt.Sprintf("invalid value %q: must be off, safe, or strict", r.Sanitize),
		}
	}

	if r.MaxPasteBytes <= 0 {
		return &ValidationError{
			Field:   "max_paste_bytes",
			Message: fmt.Sprintf("must be positive, got %d", r.MaxPasteBytes),
		}
	}

	return nil
}

func parseReservedKey(raw string) (ReservedKey, error) {
	if raw == "" {
		return ReservedKey{Disabled: true}, nil
	}

	lower := strings.ToLower(raw)
	if canonical, ok := symbolicKeys[lower]; ok {
		return ReservedKey{Symbolic: canonical}, nil
	}

	if !utf8.ValidString(raw) {
		return ReservedKey{}, fmt.Errorf("invalid UTF-8 value %q", raw)
	}
	if utf8.RuneCountInString(raw) != 1 {
		return ReservedKey{}, fmt.Errorf("must be a single printable character or Esc/Enter/Tab/Space, got %q", raw)
	}
	r, _ := utf8.DecodeRuneInString(raw)
	if !unicode.IsPrint(r) {
		return ReservedKey{}, fmt.Errorf("not a printable character: %q", raw)
	}
	return ReservedKey{Printable: r}, nil
}

func normalizePool(poolStr string, reservedPrintable map[rune]string) []rune {
	var out []rune
	seen := make(map[rune]struct{})
	for _, r := range poolStr {
		r = unicode.ToLower(r)
		if _, isReserved := reservedPrintable[r]; isReserved {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

func expandHome(path string) string {
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}
