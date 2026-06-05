package wispr

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver registered as "sqlite"
)

// snippetQuery selects the live snippets: Dictionary rows that are snippets and
// not deleted. Columns map to Snippet fields (id, phrase, replacement, starred).
const snippetQuery = `SELECT id, phrase, replacement, isStarred FROM Dictionary WHERE isSnippet = 1 AND isDeleted = 0`

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
// opened in place via the DSN `file:<abs>?mode=ro` — never copied, never
// written; mode=ro is WAL-aware. NULL replacement/phrase scan to empty strings;
// the empty-replacement skip is applied later by the mapping layer.
func (r *dbReader) Snippets() ([]Snippet, error) {
	abs, err := filepath.Abs(r.path)
	if err != nil {
		return nil, fmt.Errorf("resolve wispr db path %s: %w", r.path, err)
	}

	db, err := sql.Open("sqlite", readOnlyDSN(abs))
	if err != nil {
		return nil, fmt.Errorf("open wispr db %s: %w", abs, err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(snippetQuery)
	if err != nil {
		return nil, fmt.Errorf("query wispr snippets: %w", err)
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
			return nil, fmt.Errorf("scan wispr snippet: %w", err)
		}
		snippets = append(snippets, Snippet{
			ID:          id,
			Phrase:      phrase.String,
			Replacement: replacement.String,
			Starred:     starred.Int64 != 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wispr snippets: %w", err)
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
