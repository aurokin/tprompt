//go:build ignore

// Command gen regenerates the checked-in Wispr Flow fixture database used by the
// import_wispr_*.txtar testscripts. It writes a flow.sqlite shaped like Wispr's
// real DB (a Dictionary table) containing a deterministic mix of live snippets,
// a non-snippet row, and a deleted snippet, so the importer's filtering is
// exercised end-to-end against a real modernc-built database.
//
// The fixture is a binary blob that cannot live inside a .txtar (which is
// UTF-8/line oriented), so it is committed alongside this generator. Regenerate
// after changing the dataset with:
//
//	go run ./cmd/tprompt/testdata/wispr/gen.go
//
// The default DELETE journal mode keeps the result a single self-contained file
// (no -wal/-shm sidecars) so the committed fixture is exactly one file.
package main

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func main() {
	out := filepath.Join("cmd", "tprompt", "testdata", "wispr", "flow.sqlite")
	if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
		log.Fatalf("remove old fixture: %v", err)
	}

	db, err := sql.Open("sqlite", out)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(`CREATE TABLE Dictionary (
		id TEXT PRIMARY KEY,
		phrase TEXT,
		replacement TEXT,
		isSnippet INTEGER,
		isDeleted INTEGER,
		isStarred INTEGER
	);`); err != nil {
		log.Fatalf("create table: %v", err)
	}

	rows := []struct {
		id, phrase, replacement         string
		isSnippet, isDeleted, isStarred int
	}{
		{"11111111-1111-1111-1111-111111111111", "organize thoughts prompt", "Help me organize my thoughts into a clear outline.", 1, 0, 0},
		{"22222222-2222-2222-2222-222222222222", "code review", "Review this code for correctness, risk, and missing tests.", 1, 0, 1},
		{"33333333-3333-3333-3333-333333333333", "a dictionary word", "this is a plain dictionary expansion, not a snippet", 0, 0, 0},
		{"44444444-4444-4444-4444-444444444444", "old snippet", "a deleted snippet body", 1, 1, 0},
	}
	for _, r := range rows {
		if _, err := db.Exec(
			`INSERT INTO Dictionary (id, phrase, replacement, isSnippet, isDeleted, isStarred) VALUES (?, ?, ?, ?, ?, ?)`,
			r.id, r.phrase, r.replacement, r.isSnippet, r.isDeleted, r.isStarred,
		); err != nil {
			log.Fatalf("insert %s: %v", r.id, err)
		}
	}
	log.Printf("wrote %s", out)
}
