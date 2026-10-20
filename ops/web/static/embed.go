// Package static embeds the CSS for the server-rendered auth pages so the
// binary serves it itself, which keeps the strict self-only CSP possible. (The
// legacy htmx/sse JS is gone with the old templ UI; the app is the SPA now.)
package static

import "embed"

// FS holds the static assets served under /static/.
//
//go:embed *.css
var FS embed.FS
