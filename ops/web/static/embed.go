// Package static embeds the panel's built CSS and htmx JavaScript so the
// binary serves them itself, which keeps the strict self-only CSP possible.
package static

import "embed"

// FS holds the static assets served under /static/.
//
//go:embed *.css *.js
var FS embed.FS
