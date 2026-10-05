package server

import (
	"context"
	"io"
	"net/http"

	"github.com/a-h/templ"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// storeUser is a short alias used in handler signatures.
type storeUser = store.User

// templComponent aliases templ.Component so handler files need not import templ.
type templComponent = templ.Component

// render writes a templ component as the HTML response.
func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = c.Render(r.Context(), w)
}

// rawHTML wraps a trusted HTML string (e.g. gorilla/csrf's hidden field) as a
// templ.Component so it can be embedded in a templ template.
func rawHTML(html string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, html)
		return err
	})
}
