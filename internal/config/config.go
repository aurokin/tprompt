// Package config loads and validates tprompt's TOML configuration.
package config

// Config mirrors the user-facing fields documented in docs/storage/config.md.
// Phase 2 will populate defaults and validation; the struct exists now so other
// packages can depend on its shape.
type Config struct {
	PromptsDir                 string            `toml:"prompts_dir"`
	DefaultMode                string            `toml:"default_mode"`
	DefaultEnter               bool              `toml:"default_enter"`
	SocketPath                 string            `toml:"socket_path"`
	LogPath                    string            `toml:"log_path"`
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
