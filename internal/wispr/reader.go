package wispr

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver registered as "sqlite"
)

// snippetQuery selects the live snippets: Dictionary rows that are snippets and
// not deleted. Columns map to Snippet fields (id, phrase, replacement, starred).
// ORDER BY id makes iteration deterministic so intra-batch id disambiguation and
// the import output are stable across runs.
const snippetQuery = `SELECT id, phrase, replacement, isStarred FROM Dictionary WHERE isSnippet = 1 AND isDeleted = 0 ORDER BY id`

// dbReader reads live snippets from a Wispr Flow flow.sqlite, opened read-only.
type dbReader struct {
	path string
}

// NewReader returns a Reader over the flow.sqlite at path. Construction is lazy:
// the database is opened (read-only) only when Snippets is called, so every DB
// error (missing file, locked, permission denied) surfaces from one place.
func NewReader(path string) Reader {
	return &dbReader{path: path}
}

// Snippets opens the database read-only and returns its live snippets. The DB is
// opened in place via a read-only URI DSN — never copied, never written; mode=ro
// is WAL-aware. NULL replacement/phrase scan to empty strings; the
// empty-replacement skip is applied later by the mapping layer.
//
// Errors are classified for the exit-code contract: a missing file is a
// DBNotFoundError (usage, exit 2); any other open/read failure — a locked DB
// (Wispr running), a permission error (macOS Full Disk Access), a schema that
// lacks the expected columns — is a DBOpenError (general, exit 1).
func (r *dbReader) Snippets() ([]Snippet, error) {
	abs, err := filepath.Abs(r.path)
	if err != nil {
		return nil, fmt.Errorf("resolve wispr db path %s: %w", r.path, err)
	}
	if _, err := os.Stat(abs); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &DBNotFoundError{Path: abs}
		}
		// Permission denied (e.g. macOS Full Disk Access not granted) or another
		// stat failure: the path exists in a form we cannot read.
		return nil, &DBOpenError{Path: abs, Err: err}
	}

	db, err := sql.Open("sqlite", readOnlyDSN(abs))
	if err != nil {
		return nil, &DBOpenError{Path: abs, Err: err}
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(snippetQuery)
	if err != nil {
		return nil, &DBOpenError{Path: abs, Err: err}
	}
	defer func() { _ = rows.Close() }()

	var snippets []Snippet
	for rows.Next() {
		var (
			id          string
			phrase      sql.NullString
			replacement sql.NullString
			starred     sql.NullInt64
		)
		if err := rows.Scan(&id, &phrase, &replacement, &starred); err != nil {
			return nil, &DBOpenError{Path: abs, Err: err}
		}
		snippets = append(snippets, Snippet{
			ID:          id,
			Phrase:      phrase.String,
			Replacement: replacement.String,
			Starred:     starred.Int64 != 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, &DBOpenError{Path: abs, Err: err}
	}
	return snippets, nil
}

// readOnlyDSN builds a read-only SQLite URI DSN for an absolute path. modernc
// parses the DSN as a SQLite URI, so the path must be a proper file: URI:
// forward slashes and percent-encoded so a path containing spaces (the macOS
// default lives under "Application Support/Wispr Flow"), URI metacharacters
// (%, ?, #), or — on Windows — backslashes cannot corrupt the DSN. mode=ro opens
// in place without writing (WAL-aware).
func readOnlyDSN(abs string) string {
	p := filepath.ToSlash(abs)
	if p == "" || p[0] != '/' {
		// Windows drive-letter paths ("C:/...") need a leading slash: file:/C:/...
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}
	return u.String()
}
