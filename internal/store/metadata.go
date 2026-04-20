package store

import (
	"github.com/hsadler/tprompt/internal/promptmeta"
	"github.com/hsadler/tprompt/internal/sanitize"
)

// sanitizeMeta cleans metadata fields only. Body sanitization is deferred to
// delivery time so the author's bytes aren't altered behind their back.
//
// Metadata uses the all-escapes strip (not safe mode): any CSI in a rendered
// title/description/tag — even cosmetic SGR or cursor movement — is a display
// corruption vector in show/TUI, so we drop everything ESC-initiated.
func sanitizeMeta(meta *promptmeta.Meta) {
	meta.Title = stripEscapes(meta.Title)
	meta.Description = stripEscapes(meta.Description)
	for i, tag := range meta.Tags {
		meta.Tags[i] = stripEscapes(tag)
	}
}

func stripEscapes(value string) string {
	if value == "" {
		return value
	}
	return string(sanitize.StripAll([]byte(value)))
}
