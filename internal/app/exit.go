package app

import (
	"errors"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/keybind"
	"github.com/hsadler/tprompt/internal/store"
)

// Exit codes documented in docs/commands/cli.md.
const (
	ExitOK       = 0
	ExitGeneral  = 1
	ExitUsage    = 2
	ExitPrompt   = 3
	ExitTmux     = 4
	ExitDaemon   = 5
	ExitDelivery = 6
)

// ExitCode maps an error to the appropriate process exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}

	var configErr *config.ValidationError
	if errors.As(err, &configErr) {
		return ExitUsage
	}

	var missingDir *store.PromptsDirMissingError
	if errors.As(err, &missingDir) {
		return ExitUsage
	}

	var dupID *store.DuplicatePromptIDError
	if errors.As(err, &dupID) {
		return ExitPrompt
	}
	var notFound *store.NotFoundError
	if errors.As(err, &notFound) {
		return ExitPrompt
	}
	var invalidMode *store.InvalidPromptModeError
	if errors.As(err, &invalidMode) {
		return ExitPrompt
	}

	var dupKey *keybind.DuplicateKeybindError
	if errors.As(err, &dupKey) {
		return ExitPrompt
	}
	var reservedKey *keybind.ReservedKeybindError
	if errors.As(err, &reservedKey) {
		return ExitPrompt
	}
	var malformedKey *keybind.MalformedKeybindError
	if errors.As(err, &malformedKey) {
		return ExitPrompt
	}

	return ExitGeneral
}
