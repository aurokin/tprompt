package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

func matchesReserved(msg tea.KeyMsg, binding ReservedBinding) bool {
	if binding.Disabled {
		return false
	}
	if binding.Symbolic != "" {
		return matchesSymbolic(msg, binding.Symbolic)
	}
	if binding.Printable == 0 || msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return false
	}
	got := msg.Runes[0]
	if unicode.IsLetter(binding.Printable) && unicode.IsLetter(got) {
		return unicode.ToLower(got) == unicode.ToLower(binding.Printable)
	}
	return got == binding.Printable
}

func matchesSymbolic(msg tea.KeyMsg, symbolic string) bool {
	switch symbolic {
	case "Esc":
		return msg.Type == tea.KeyEsc
	case "Enter":
		return msg.Type == tea.KeyEnter
	case "Tab":
		return msg.Type == tea.KeyTab
	case "Space":
		return msg.Type == tea.KeySpace || (msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == ' ')
	default:
		return false
	}
}

func displayReserved(binding ReservedBinding) string {
	if binding.Disabled {
		return ""
	}
	if binding.Symbolic != "" {
		return binding.Symbolic
	}
	if binding.Printable == 0 {
		return ""
	}
	s := string(binding.Printable)
	if unicode.IsLetter(binding.Printable) {
		return strings.ToUpper(s)
	}
	return s
}

func footerHint(binding ReservedBinding, action string) string {
	label := displayReserved(binding)
	if label == "" {
		return ""
	}
	return "[" + label + " " + action + "]"
}
