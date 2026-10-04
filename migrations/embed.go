// Package migrations embeds the SQL files so they ship inside the app binary.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
