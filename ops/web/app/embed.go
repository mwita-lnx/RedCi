// Package app embeds the built React SPA (Vite output in dist/) so the panel
// binary serves it under /app/. Run `npm run build` in this directory (the
// Dockerfile does this automatically) before `go build`.
package app

import "embed"

// Dist holds the built SPA assets. The dist/ directory must exist at build
// time; a .gitkeep keeps it present for local `go build` before a first build.
//
//go:embed all:dist
var Dist embed.FS
