package store

import (
	"github.com/hsadler/tprompt/internal/promptmeta"
	"github.com/hsadler/tprompt/internal/sanitize"
)

// sanitizeMeta cleans metadata fields only. Body sanitization is deferred to
// delivery time so the author's bytes aren't altered behind their back.
func sanitizeMeta(meta *promptmeta.Meta) {
	s := sanitize.New(sanitize.ModeSafe)
	meta.Title = stripUnsafeEscapes(s, meta.Title)
	meta.Description = stripUnsafeEscapes(s, meta.Description)
	for i, tag := range meta.Tags {
		meta.Tags[i] = stripUnsafeEscapes(s, tag)
	}
}

func stripUnsafeEscapes(s sanitize.Sanitizer, value string) string {
	if value == "" {
		return value
	}
	cleaned, _ := s.Process([]byte(value))
	return string(cleaned)
}
