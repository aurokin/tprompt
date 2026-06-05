package wispr

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

// writableDSN mirrors readOnlyDSN's encoding but opens read-write-create, so the
// fixture builder can write to awkward paths (spaces, URI metacharacters) too.
func writableDSN(abs string) string {
	p := filepath.ToSlash(abs)
	if p == "" || p[0] != '/' {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=rwc"}
	return u.String()
}

// buildFixtureDB writes a flow.sqlite at path with a Dictionary table shaped like
// Wispr's, containing a mix of live snippets, a non-snippet, and a deleted
// snippet, so a reader must filter to exactly the live snippets. It is built via
// modernc (the same driver the production reader uses), in the default DELETE
// journal mode, so the result is a single self-contained file with no WAL sidecar.
func buildFixtureDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", writableDSN(path))
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	defer func() { _ = db.Close() }()

	const schema = `CREATE TABLE Dictionary (
		id TEXT PRIMARY KEY,
		phrase TEXT,
		replacement TEXT,
		isSnippet INTEGER,
		isDeleted INTEGER,
		isStarred INTEGER
	);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	rows := []struct {
		id, phrase, replacement         string
		isSnippet, isDeleted, isStarred int
	}{
		{"uuid-live-1", "organize thoughts prompt", "Help me organize my thoughts.", 1, 0, 0},
		{"uuid-live-2", "code review", "Review this code for correctness.", 1, 0, 1},
		{"uuid-nonsnippet", "a dictionary word", "expansion", 0, 0, 0},
		{"uuid-deleted", "old snippet", "stale body", 1, 1, 0},
	}
	for _, r := range rows {
		if _, err := db.Exec(
			`INSERT INTO Dictionary (id, phrase, replacement, isSnippet, isDeleted, isStarred) VALUES (?, ?, ?, ?, ?, ?)`,
			r.id, r.phrase, r.replacement, r.isSnippet, r.isDeleted, r.isStarred,
		); err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}
}

func TestReader_ReturnsOnlyLiveSnippets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flow.sqlite")
	buildFixtureDB(t, path)

	snips, err := NewReader(path).Snippets()
	if err != nil {
		t.Fatalf("Snippets: %v", err)
	}
	sort.Slice(snips, func(i, j int) bool { return snips[i].ID < snips[j].ID })

	if len(snips) != 2 {
		t.Fatalf("got %d snippets, want 2 (non-snippet and deleted rows excluded): %+v", len(snips), snips)
	}
	if snips[0].ID != "uuid-live-1" || snips[0].Phrase != "organize thoughts prompt" {
		t.Errorf("snippet[0] = %+v", snips[0])
	}
	if snips[1].ID != "uuid-live-2" || !snips[1].Starred {
		t.Errorf("snippet[1] = %+v, want starred live snippet", snips[1])
	}
}

func TestReader_OpensReadOnlyAndNeverWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flow.sqlite")
	buildFixtureDB(t, path)

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if _, err := NewReader(path).Snippets(); err != nil {
		t.Fatalf("Snippets: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if before.ModTime() != after.ModTime() || before.Size() != after.Size() {
		t.Errorf("flow.sqlite was modified by a read-only open (mtime/size changed)")
	}
	// mode=ro must not leave a WAL sidecar next to the DB.
	if _, err := os.Stat(path + "-wal"); err == nil {
		t.Errorf("read-only open created a -wal sidecar")
	}
}

func TestReader_MissingDBErrors(t *testing.T) {
	_, err := NewReader(filepath.Join(t.TempDir(), "nope.sqlite")).Snippets()
	if err == nil {
		t.Fatal("Snippets on a missing DB = nil error, want error")
	}
}

func TestReader_HandlesAwkwardPaths(t *testing.T) {
	// The DSN must survive paths with spaces (the macOS default lives under
	// "Application Support/Wispr Flow") and URI metacharacters (%, ?, #) that a
	// user-supplied --db-path might contain, rather than corrupting the SQLite URI.
	dirs := map[string]string{
		"spaces":    "Application Support/Wispr Flow",
		"uri-metas": "weird #1 ? dir %2",
		"mixed":     "a b%c?d#e",
	}
	for name, sub := range dirs {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), sub)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			path := filepath.Join(dir, "flow.sqlite")
			buildFixtureDB(t, path)
			snips, err := NewReader(path).Snippets()
			if err != nil {
				t.Fatalf("Snippets(%q): %v", path, err)
			}
			if len(snips) != 2 {
				t.Errorf("got %d snippets, want 2", len(snips))
			}
		})
	}
}
