package app

import (
	"errors"
	"strings"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/keybind"
	"github.com/hsadler/tprompt/internal/promptsource"
	"github.com/hsadler/tprompt/internal/sanitize"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/submitter"
	"github.com/hsadler/tprompt/internal/tmux"
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
//
//nolint:funlen,gocognit // flat errors.As dispatch is clearest as a sequence of branches; funlen (length) and gocognit (branch count) both flag this shape unavoidably.
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

	var unresolvedDefault *promptsource.UnresolvedDefaultDirError
	if errors.As(err, &unresolvedDefault) {
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

	var strictReject *sanitize.StrictRejectError
	if errors.As(err, &strictReject) {
		return ExitPrompt
	}

	var bodyTooLarge *submitter.BodyTooLargeError
	if errors.As(err, &bodyTooLarge) {
		return ExitPrompt
	}

	if errors.Is(err, clipboard.ErrNoReaderAvailable) {
		return ExitPrompt
	}
	var clipRead *clipboard.ReadError
	if errors.As(err, &clipRead) {
		return ExitPrompt
	}

	// Clipboard content errors from the TUI clipboard row and `tprompt paste`.
	// The Submitter translates oversize clipboard content to
	// *submitter.BodyTooLargeError; `paste` sees clipboard.OversizeError
	// directly from clipboard.Validate.
	var clipEmpty *clipboard.EmptyClipboardError
	if errors.As(err, &clipEmpty) {
		return ExitPrompt
	}
	var clipUTF8 *clipboard.InvalidUTF8Error
	if errors.As(err, &clipUTF8) {
		return ExitPrompt
	}
	var clipOversize *clipboard.OversizeError
	if errors.As(err, &clipOversize) {
		return ExitPrompt
	}

	var socketErr *daemon.SocketUnavailableError
	if errors.As(err, &socketErr) {
		return ExitDaemon
	}
	var ipcErr *daemon.IPCError
	if errors.As(err, &ipcErr) {
		return ExitDaemon
	}
	var shutdownTimeoutErr *daemon.ShutdownTimeoutError
	if errors.As(err, &shutdownTimeoutErr) {
		return ExitDaemon
	}
	var timeoutErr *daemon.TimeoutError
	if errors.As(err, &timeoutErr) {
		return ExitDaemon
	}
	var policyErr *daemon.InvalidPolicyError
	if errors.As(err, &policyErr) {
		return ExitDaemon
	}

	var envErr *tmux.EnvError
	if errors.As(err, &envErr) {
		return ExitTmux
	}
	var paneMissing *tmux.PaneMissingError
	if errors.As(err, &paneMissing) {
		return ExitTmux
	}
	var deliveryErr *tmux.DeliveryError
	if errors.As(err, &deliveryErr) {
		return ExitDelivery
	}
	var oversizeErr *tmux.OversizeError
	if errors.As(err, &oversizeErr) {
		return ExitDelivery
	}

	if isCobraUsageError(err) {
		return ExitUsage
	}

	return ExitGeneral
}

// isCobraUsageError recognizes the plain-text errors cobra/pflag emit for
// flag and arg validation failures (required flag missing, unknown flag,
// unknown subcommand, wrong arg count). Cobra doesn't return typed errors
// for these, so string-matching is the established pattern.
func isCobraUsageError(err error) bool {
	msg := err.Error()
	switch {
	case strings.Contains(msg, `required flag(s)`):
		return true
	case strings.HasPrefix(msg, "unknown flag"):
		return true
	case strings.HasPrefix(msg, "unknown shorthand flag"):
		return true
	case strings.HasPrefix(msg, "unknown command"):
		return true
	case strings.HasPrefix(msg, "flag needs an argument"):
		return true
	case strings.HasPrefix(msg, "bad flag syntax"):
		return true
	case strings.HasPrefix(msg, "invalid argument"):
		return true
	case strings.Contains(msg, "arg(s), received"):
		return true
	case strings.Contains(msg, "accepts "):
		return true
	}
	return false
}
