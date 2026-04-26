package promptmeta

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFrontmatterAndBody(t *testing.T) {
	content := readFixture(t, "code-review.md")

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.Meta.Title != "Code Review" {
		t.Fatalf("Title = %q, want %q", parsed.Meta.Title, "Code Review")
	}
	if parsed.Meta.Description != "Deep review prompt focused on correctness, risk, tests" {
		t.Fatalf("Description = %q", parsed.Meta.Description)
	}
	if parsed.Meta.Key == nil || *parsed.Meta.Key != "c" {
		t.Fatalf("Key = %v, want %q", parsed.Meta.Key, "c")
	}
	if !parsed.Meta.KeyDeclared {
		t.Fatal("KeyDeclared = false, want true")
	}
	if parsed.Meta.Mode != "paste" {
		t.Fatalf("Mode = %q, want %q", parsed.Meta.Mode, "paste")
	}
	if parsed.Meta.Enter == nil || *parsed.Meta.Enter {
		t.Fatalf("Enter = %v, want false", parsed.Meta.Enter)
	}
	wantBody := "Review this code for correctness, risk, and missing tests.\n"
	if parsed.Body != wantBody {
		t.Fatalf("Body = %q, want %q", parsed.Body, wantBody)
	}
}

func TestParseFrontmatterAndBodyWithCRLF(t *testing.T) {
	content := []byte("---\r\ntitle: Demo\r\nkey: c\r\n---\r\n\r\nBody\r\n")

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.Meta.Title != "Demo" {
		t.Fatalf("Title = %q, want %q", parsed.Meta.Title, "Demo")
	}
	if parsed.Meta.Key == nil || *parsed.Meta.Key != "c" {
		t.Fatalf("Key = %v, want %q", parsed.Meta.Key, "c")
	}
	if !parsed.Meta.KeyDeclared {
		t.Fatal("KeyDeclared = false, want true")
	}
	if parsed.Body != "Body\r\n" {
		t.Fatalf("Body = %q, want %q", parsed.Body, "Body\r\n")
	}
}

func TestParseFrontmatterAndBodyWithUTF8BOM(t *testing.T) {
	content := []byte("\ufeff---\ntitle: Demo\nkey: c\n---\nBody\n")

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.Meta.Title != "Demo" {
		t.Fatalf("Title = %q, want %q", parsed.Meta.Title, "Demo")
	}
	if parsed.Meta.Key == nil || *parsed.Meta.Key != "c" {
		t.Fatalf("Key = %v, want %q", parsed.Meta.Key, "c")
	}
	if !parsed.Meta.KeyDeclared {
		t.Fatal("KeyDeclared = false, want true")
	}
	if parsed.Body != "Body\n" {
		t.Fatalf("Body = %q, want %q", parsed.Body, "Body\n")
	}
}

func TestParseWithoutFrontmatterReturnsWholeBody(t *testing.T) {
	content := readFixture(t, "no-frontmatter.md")

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	wantBody := "Just a body, no frontmatter. ID is derived from the filename stem.\n"
	if parsed.Body != wantBody {
		t.Fatalf("Body = %q, want %q", parsed.Body, wantBody)
	}
	if parsed.Meta.KeyDeclared {
		t.Fatal("KeyDeclared = true, want false")
	}
}

func TestParseIgnoresUnknownFields(t *testing.T) {
	parsed, err := Parse([]byte("---\ntitle: Demo\nunknown: ignored\n---\nbody\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Meta.Title != "Demo" {
		t.Fatalf("Title = %q, want %q", parsed.Meta.Title, "Demo")
	}
	if parsed.Body != "body\n" {
		t.Fatalf("Body = %q, want %q", parsed.Body, "body\n")
	}
}

func TestParseTreatsLeadingFenceWithoutClosingFenceAsBody(t *testing.T) {
	content := []byte("---\nHeading below\n")

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.Body != string(content) {
		t.Fatalf("Body = %q, want %q", parsed.Body, string(content))
	}
	if parsed.Meta.Title != "" || parsed.Meta.Description != "" || len(parsed.Meta.Tags) != 0 ||
		parsed.Meta.Mode != "" || parsed.Meta.Enter != nil || parsed.Meta.Key != nil || parsed.Meta.KeyDeclared {
		t.Fatalf("Meta = %#v, want zero value", parsed.Meta)
	}
}

func TestParseTreatsNonMappingFenceAsBody(t *testing.T) {
	content := []byte("---\nHeading\n---\nbody\n")

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.Body != string(content) {
		t.Fatalf("Body = %q, want %q", parsed.Body, string(content))
	}
	if parsed.Meta.Title != "" || parsed.Meta.Description != "" || len(parsed.Meta.Tags) != 0 ||
		parsed.Meta.Mode != "" || parsed.Meta.Enter != nil || parsed.Meta.Key != nil || parsed.Meta.KeyDeclared {
		t.Fatalf("Meta = %#v, want zero value", parsed.Meta)
	}
}

func TestParseRejectsMalformedMappingFrontmatter(t *testing.T) {
	_, err := Parse([]byte("---\ntitle: [\n---\nbody\n"))
	if err == nil {
		t.Fatal("Parse: want error, got nil")
	}
}

func TestParseTrimsOnlyOneLeadingLineBreakAfterFence(t *testing.T) {
	parsed, err := Parse([]byte("---\ntitle: Demo\n---\n\n\nbody\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Body != "\nbody\n" {
		t.Fatalf("Body = %q, want %q", parsed.Body, "\nbody\n")
	}
}

func TestParseTreatsEmptyKeyAsAbsent(t *testing.T) {
	tests := map[string]string{
		"implicit-null": "---\nkey:\n---\nbody\n",
		"explicit-null": "---\nkey: null\n---\nbody\n",
		"empty-string":  "---\nkey: \"\"\n---\nbody\n",
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			parsed, err := Parse([]byte(content))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if parsed.Meta.Key != nil {
				t.Fatalf("Key = %v, want nil", parsed.Meta.Key)
			}
			if parsed.Meta.KeyDeclared {
				t.Fatal("KeyDeclared = true, want false")
			}
		})
	}
}

func TestParseTreatsEmptyModeAsAbsent(t *testing.T) {
	tests := map[string]string{
		"implicit-null": "---\nmode:\n---\nbody\n",
		"empty-string":  "---\nmode: \"\"\n---\nbody\n",
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			parsed, err := Parse([]byte(content))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if parsed.Meta.Mode != "" {
				t.Fatalf("Mode = %q, want %q", parsed.Meta.Mode, "")
			}
		})
	}
}

func TestParseTreatsEmptyAndElidedTagsIdentically(t *testing.T) {
	tests := map[string]string{
		"implicit-null": "---\ntags:\n---\nbody\n",
		"empty-list":    "---\ntags: []\n---\nbody\n",
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			parsed, err := Parse([]byte(content))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(parsed.Meta.Tags) != 0 {
				t.Fatalf("Tags = %#v, want empty", parsed.Meta.Tags)
			}
		})
	}
}

func TestParseEnterEmptyValueRemainsNil(t *testing.T) {
	parsed, err := Parse([]byte("---\nenter:\n---\nbody\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Meta.Enter != nil {
		t.Fatalf("Enter = %v, want nil", parsed.Meta.Enter)
	}
}

func TestParseStubEmptyFixtureLoadsCleanly(t *testing.T) {
	content := readFixture(t, "stub-empty.md")

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.Meta.Title != "" || parsed.Meta.Description != "" {
		t.Fatalf("display fields not empty: %#v", parsed.Meta)
	}
	if len(parsed.Meta.Tags) != 0 {
		t.Fatalf("Tags = %#v, want empty", parsed.Meta.Tags)
	}
	if parsed.Meta.Key != nil || parsed.Meta.KeyDeclared {
		t.Fatalf("Key = %v, KeyDeclared = %v, want absent", parsed.Meta.Key, parsed.Meta.KeyDeclared)
	}
	if parsed.Meta.Mode != "" {
		t.Fatalf("Mode = %q, want empty", parsed.Meta.Mode)
	}
	if parsed.Meta.Enter != nil {
		t.Fatalf("Enter = %v, want nil", parsed.Meta.Enter)
	}
	if parsed.Body != "Stubbed-empty body.\n" {
		t.Fatalf("Body = %q", parsed.Body)
	}
}

func TestParseEmptyKeyMixedWithPopulatedFields(t *testing.T) {
	content := []byte("---\ntitle: Mixed\ndescription: hi\ntags: [one]\nkey:\nmode: paste\nenter: false\n---\nbody\n")

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Meta.Title != "Mixed" || parsed.Meta.Description != "hi" {
		t.Fatalf("display fields = %#v", parsed.Meta)
	}
	if len(parsed.Meta.Tags) != 1 || parsed.Meta.Tags[0] != "one" {
		t.Fatalf("Tags = %#v", parsed.Meta.Tags)
	}
	if parsed.Meta.Key != nil || parsed.Meta.KeyDeclared {
		t.Fatalf("Key = %v, KeyDeclared = %v, want absent", parsed.Meta.Key, parsed.Meta.KeyDeclared)
	}
	if parsed.Meta.Mode != "paste" {
		t.Fatalf("Mode = %q, want paste", parsed.Meta.Mode)
	}
	if parsed.Meta.Enter == nil || *parsed.Meta.Enter {
		t.Fatalf("Enter = %v, want false", parsed.Meta.Enter)
	}
}

func TestParseEmptyTitleAndDescriptionRemainAcceptedAsDisplayValues(t *testing.T) {
	content := []byte("---\ntitle: \"\"\ndescription: \"\"\n---\nbody\n")

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Meta.Title != "" {
		t.Fatalf("Title = %q, want empty", parsed.Meta.Title)
	}
	if parsed.Meta.Description != "" {
		t.Fatalf("Description = %q, want empty", parsed.Meta.Description)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join("..", "..", "testdata", "prompts", name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return content
}
