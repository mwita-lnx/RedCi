package server

import (
	"io/fs"
	"net/http"
	"strings"

	spa "github.com/mwita-lnx/RedCi/ops/web/app"
)

// spaFS is the embedded SPA dist/ rooted so paths are served without the dist/
// prefix (e.g. /app/assets/x.js -> dist/assets/x.js).
var spaFS, _ = fs.Sub(spa.Dist, "dist")

// handleSPA serves the React SPA under /app/. Real asset requests are served
// from the embedded FS; any other path (client-side route, deep link, refresh)
// falls back to index.html so the browser router can take over.
func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	// Strip the /app prefix to get the asset path within dist/.
	p := strings.TrimPrefix(r.URL.Path, "/app")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		p = "index.html"
	}

	// If the requested file exists in the bundle, serve it. Otherwise fall back
	// to index.html for SPA routing (e.g. /app/sites/3 on a hard refresh).
	if f, err := spaFS.Open(p); err == nil {
		_ = f.Close()
		// Long-cache fingerprinted assets; never cache index.html.
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFileFS(w, r, spaFS, p)
		return
	}
	// Fallback: index.html (no cache so new deploys are picked up).
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFileFS(w, r, spaFS, "index.html")
}
