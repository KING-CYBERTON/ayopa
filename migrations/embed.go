// Package migrations embeds the SQL files so they ship inside the app binary.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed *.sql
var FS embed.FS

// FileNames returns the sorted list of embedded SQL file names.
// Tests use this to derive expected migration counts without hardcoding them.
func FileNames() []string {
	entries, err := fs.Glob(FS, "*.sql")
	if err != nil {
		panic(err)
	}
	return entries
}
