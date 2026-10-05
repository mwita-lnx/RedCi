// Package agentmigrations embeds the agent's goose migrations.
package agentmigrations

import "embed"

// FS holds the agent's local journal migrations.
//
//go:embed *.sql
var FS embed.FS
