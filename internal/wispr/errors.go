package wispr

import "fmt"

// DBNotFoundError reports that no Wispr database exists at the resolved path.
// Surfaced as a usage error (exit 2) by app.ExitCode, mirroring a missing
// prompts directory; the message points the user at --db-path.
type DBNotFoundError struct {
	Path string
}

func (e *DBNotFoundError) Error() string {
	return fmt.Sprintf("wispr database not found at %s; pass --db-path to point at your flow.sqlite", e.Path)
}

// DBPathRequiredError reports that the current OS has no default Wispr database
// location, so --db-path is required. Usage error (exit 2).
type DBPathRequiredError struct {
	OS string
}

func (e *DBPathRequiredError) Error() string {
	return fmt.Sprintf("no default Wispr Flow database location on %s; pass --db-path to point at your flow.sqlite", e.OS)
}

// DBOpenError reports that the Wispr database exists but could not be opened or
// read — most often because Wispr Flow is running (the database is locked) or,
// on macOS, the terminal has not been granted Full Disk Access. General error
// (exit 1); the message is actionable.
type DBOpenError struct {
	Path string
	Err  error
}

func (e *DBOpenError) Error() string {
	return fmt.Sprintf(
		"could not read Wispr database %s: %v; if Wispr Flow is running, quit it or point --db-path at a copy (on macOS, grant your terminal Full Disk Access)",
		e.Path, e.Err,
	)
}

func (e *DBOpenError) Unwrap() error { return e.Err }
