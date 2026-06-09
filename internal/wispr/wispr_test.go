package wispr

import (
	"strings"
	"testing"

	"github.com/aurokin/tprompt/internal/promptmeta"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name   string
		phrase string
		want   string
	}{
		{"simple", "organize thoughts prompt", "organize-thoughts-prompt"},
		{"already-slug", "code-review", "code-review"},
		{"underscores", "deep_review_v2", "deep-review-v2"},
		{"mixed-case", "Code Review", "code-review"},
		{"collapse-spaces", "a   b", "a-b"},
		{"trim-edges", "  hello  ", "hello"},
		{"strip-punctuation", "what's up?!", "whats-up"},
		{"colon-in-phrase", "title: thing", "title-thing"},
		{"unicode-dropped", "café ☕ time", "caf-time"},
		{"all-punctuation-empty", "!@#$%", ""},
		{"leading-trailing-dash", "--x--", "x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := slugify(tc.phrase); got != tc.want {
				t.Errorf("slugify(%q) = %q, want %q", tc.phrase, got, tc.want)
			}
		})
	}
}

func TestToPrompt_FrontmatterAndBodyRoundTrip(t *testing.T) {
	s := Snippet{
		ID:          "11111111-2222-3333-4444-555555555555",
		Phrase:      "organize thoughts prompt",
		Replacement: "Help me organize my thoughts into a clear outline.",
	}
	id, md, ok := s.ToPrompt("wispr")
	if !ok {
		t.Fatal("ToPrompt ok = false, want true")
	}
	if id != "organize-thoughts-prompt" {
		t.Errorf("id = %q, want %q", id, "organize-thoughts-prompt")
	}
	// The provenance tag renders in compact flow style, matching the PRD example.
	if !strings.Contains(string(md), "tags: [wispr]") {
		t.Errorf("markdown missing `tags: [wispr]`:\n%s", md)
	}

	// The file the importer writes must load cleanly through the real prompt
	// parser: title preserved verbatim, tags carried, body == replacement.
	parsed, err := promptmeta.Parse(md)
	if err != nil {
		t.Fatalf("promptmeta.Parse: %v", err)
	}
	if parsed.Meta.Title != s.Phrase {
		t.Errorf("title = %q, want %q (verbatim phrase)", parsed.Meta.Title, s.Phrase)
	}
	if len(parsed.Meta.Tags) != 1 || parsed.Meta.Tags[0] != "wispr" {
		t.Errorf("tags = %v, want [wispr]", parsed.Meta.Tags)
	}
	if parsed.Body != s.Replacement {
		t.Errorf("body = %q, want %q (== replacement)", parsed.Body, s.Replacement)
	}
}

func TestToPrompt_EmitsFullScaffoldFieldSet(t *testing.T) {
	// Imported prompts carry the same frontmatter field set `tprompt new`
	// scaffolds (AUR-526): the description/key/mode/enter stubs are added so an
	// imported prompt is as editable as a scaffolded one. The stubs are bare keys
	// (no value): bare matches the scaffold and, for `enter:`, is the only form
	// that round-trips into promptmeta.Meta's *bool without a parse error.
	s := Snippet{
		ID:          "11111111-2222-3333-4444-555555555555",
		Phrase:      "organize thoughts prompt",
		Replacement: "Help me organize my thoughts.",
	}
	_, md, ok := s.ToPrompt("wispr")
	if !ok {
		t.Fatal("ToPrompt ok = false, want true")
	}

	for _, stub := range []string{"\ndescription:\n", "\nkey:\n", "\nmode:\n", "\nenter:\n"} {
		if !strings.Contains(string(md), stub) {
			t.Errorf("frontmatter missing bare stub %q:\n%s", stub, md)
		}
	}

	// The stubbed file must load cleanly with the new fields treated as unset
	// (DECISIONS §9), not as a parse error or an empty-but-present value.
	parsed, err := promptmeta.Parse(md)
	if err != nil {
		t.Fatalf("promptmeta.Parse: %v", err)
	}
	if parsed.Meta.Description != "" {
		t.Errorf("Description = %q, want empty (unset)", parsed.Meta.Description)
	}
	if parsed.Meta.Key != nil || parsed.Meta.KeyDeclared {
		t.Errorf("Key = %v (declared=%v), want nil/undeclared (auto-assign)", parsed.Meta.Key, parsed.Meta.KeyDeclared)
	}
	if parsed.Meta.Mode != "" {
		t.Errorf("Mode = %q, want empty (unset)", parsed.Meta.Mode)
	}
	if parsed.Meta.Enter != nil {
		t.Errorf("Enter = %v, want nil (unset)", parsed.Meta.Enter)
	}
	if parsed.Body != s.Replacement {
		t.Errorf("Body = %q, want %q (unchanged by the added stubs)", parsed.Body, s.Replacement)
	}
}

func TestToPrompt_BodyMatchesReplacementThroughTrimContract(t *testing.T) {
	// Pin the promptmeta trim interaction for whitespace-bearing replacements:
	// the single trailing newline ToPrompt appends is exactly what Parse strips.
	// A replacement ending in bare '\r' is tested separately because the appended
	// '\n' forms a trailing CRLF, which promptmeta trims as one line break.
	cases := map[string]string{
		"plain":            "Review this code.",
		"multiline":        "line1\nline2",
		"trailing-newline": "ends with newline\n",
		"leading-newline":  "\nstarts with a blank line",
		"leading-spaces":   "  indented body",
		"internal-blank":   "para one\n\npara two",
		"crlf":             "windows\r\nline",
		"colon-and-quotes": `say "hi: there"`,
	}
	for name, replacement := range cases {
		t.Run(name, func(t *testing.T) {
			s := Snippet{ID: "id", Phrase: "phrase", Replacement: replacement}
			_, md, ok := s.ToPrompt("wispr")
			if !ok {
				t.Fatalf("ToPrompt ok = false for %q", replacement)
			}
			parsed, err := promptmeta.Parse(md)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if parsed.Body != replacement {
				t.Errorf("body = %q, want %q", parsed.Body, replacement)
			}
		})
	}
}

func TestToPrompt_BareCRTailFollowsPromptmetaTrimContract(t *testing.T) {
	s := Snippet{ID: "id", Phrase: "phrase", Replacement: "abc\r"}
	_, md, ok := s.ToPrompt("wispr")
	if !ok {
		t.Fatal("ToPrompt ok = false, want true")
	}
	parsed, err := promptmeta.Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Body != "abc" {
		t.Errorf("body = %q, want %q after trailing CRLF trim", parsed.Body, "abc")
	}
}

func TestSnippet_TitleAndDisambiguator(t *testing.T) {
	// Title is the verbatim phrase; Disambiguator is the first 6 slug-safe chars
	// of the UUID — the stable, non-empty seed the import engine appends to resolve
	// intra-batch id collisions (part of the source-neutral ImportRecord contract).
	s := Snippet{ID: "ABC-123-def-456", Phrase: "Organize my thoughts!"}
	if got := s.Title(); got != "Organize my thoughts!" {
		t.Errorf("Title() = %q, want the verbatim phrase", got)
	}
	if got := s.Disambiguator(); got != "abc123" {
		t.Errorf("Disambiguator() = %q, want abc123 (IDPrefix(ID, 6))", got)
	}
	if s.Disambiguator() == "" {
		t.Error("Disambiguator() must be non-empty for a real snippet")
	}
}

func TestTags_ProvenanceAndStarred(t *testing.T) {
	// Snippet.Tags is the single source of truth ToPrompt and the import picker
	// both consume: the provenance tag always, plus "starred" only when starred.
	plain := Snippet{ID: "id", Phrase: "p", Replacement: "body"}
	if got := plain.Tags("imported"); len(got) != 1 || got[0] != "imported" {
		t.Errorf("plain tags = %v, want [imported]", got)
	}
	starred := Snippet{ID: "id", Phrase: "p", Replacement: "body", Starred: true}
	if got := starred.Tags("wispr"); len(got) != 2 || got[0] != "wispr" || got[1] != "starred" {
		t.Errorf("starred tags = %v, want [wispr starred]", got)
	}
}

func TestToPrompt_StarredAppendsTag(t *testing.T) {
	s := Snippet{ID: "id", Phrase: "fav", Replacement: "body", Starred: true}
	_, md, ok := s.ToPrompt("wispr")
	if !ok {
		t.Fatal("ok = false")
	}
	parsed, err := promptmeta.Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{"wispr", "starred"}
	if len(parsed.Meta.Tags) != 2 || parsed.Meta.Tags[0] != want[0] || parsed.Meta.Tags[1] != want[1] {
		t.Errorf("tags = %v, want %v", parsed.Meta.Tags, want)
	}
}

func TestToPrompt_CustomTag(t *testing.T) {
	s := Snippet{ID: "id", Phrase: "fav", Replacement: "body"}
	_, md, _ := s.ToPrompt("imported")
	parsed, err := promptmeta.Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed.Meta.Tags) != 1 || parsed.Meta.Tags[0] != "imported" {
		t.Errorf("tags = %v, want [imported]", parsed.Meta.Tags)
	}
}

func TestToPrompt_EmptyReplacementNotImportable(t *testing.T) {
	for _, replacement := range []string{"", "   ", "\n\t "} {
		s := Snippet{ID: "id", Phrase: "x", Replacement: replacement}
		id, _, ok := s.ToPrompt("wispr")
		if ok {
			t.Errorf("ToPrompt(replacement=%q) ok = true, want false", replacement)
		}
		// Even when skipped, ToPrompt reports a valid id (for skip reporting).
		if id == "" {
			t.Errorf("ToPrompt(replacement=%q) id = %q, want non-empty", replacement, id)
		}
	}
}

func TestMustYAMLMarshalPanicsOnInvariantViolation(t *testing.T) {
	defer func() {
		got := recover()
		if got == nil {
			t.Fatal("mustYAMLMarshal did not panic for an unsupported value")
		}
		if got != "wispr frontmatter marshal invariant violated" {
			t.Fatalf("panic = %q, want static invariant message", got)
		}
	}()

	_ = mustYAMLMarshal(func() {})
}

func TestToPrompt_UnsluggablePhraseFallsBackToUUID(t *testing.T) {
	// A phrase with no sluggable characters yields a `wispr-<first8 uuid>` id,
	// while the title still preserves the original phrase verbatim.
	s := Snippet{ID: "9F8E7D6C-1111-2222-3333-444444444444", Phrase: "🔥🔥🔥", Replacement: "body"}
	id, md, ok := s.ToPrompt("wispr")
	if !ok {
		t.Fatal("ok = false")
	}
	if id != "wispr-9f8e7d6c" {
		t.Errorf("id = %q, want %q", id, "wispr-9f8e7d6c")
	}
	parsed, err := promptmeta.Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Meta.Title != "🔥🔥🔥" {
		t.Errorf("title = %q, want the original phrase", parsed.Meta.Title)
	}
}

func TestIDPrefix(t *testing.T) {
	tests := []struct {
		name string
		id   string
		n    int
		want string
	}{
		{"uuid-first8", "9f8e7d6c-1111-2222-3333-444444444444", 8, "9f8e7d6c"},
		{"uuid-first6", "9f8e7d6c-1111-2222-3333-444444444444", 6, "9f8e7d6c"[:6]},
		{"hyphens-dropped", "ab-cd-ef", 4, "abcd"},
		{"uppercase-lowered", "ABCDEF", 3, "abc"},
		{"shorter-than-n", "ab", 8, "ab"},
		{"non-hex-alnum-kept", "z9y8", 3, "z9y"},
		{"empty", "", 6, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IDPrefix(tc.id, tc.n); got != tc.want {
				t.Errorf("IDPrefix(%q, %d) = %q, want %q", tc.id, tc.n, got, tc.want)
			}
		})
	}
}

func TestDefaultDBPath(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		home   string
		want   string
		wantOK bool
	}{
		{
			name:   "darwin",
			goos:   "darwin",
			home:   "/Users/me",
			want:   "/Users/me/Library/Application Support/Wispr Flow/flow.sqlite",
			wantOK: true,
		},
		{
			name:   "darwin-no-home",
			goos:   "darwin",
			home:   "",
			wantOK: false,
		},
		{
			// No native Windows binary ships (Windows is WSL2 = the linux build),
			// so windows has no zero-config default and must use --db-path.
			name:   "windows-no-default",
			goos:   "windows",
			home:   `C:\Users\me`,
			wantOK: false,
		},
		{
			name:   "linux-no-default",
			goos:   "linux",
			home:   "/home/me",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DefaultDBPath(tc.goos, tc.home)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Errorf("path = %q, want %q", got, tc.want)
			}
		})
	}
}
