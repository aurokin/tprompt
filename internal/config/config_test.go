package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

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
