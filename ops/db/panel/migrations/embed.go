// Package migrations embeds the panel's goose SQL migrations so they ship
// inside the binary and run automatically on start.
package migrations

import "embed"

// FS holds every .sql migration for panel.db.
//
//go:embed *.sql
var FS embed.FS
