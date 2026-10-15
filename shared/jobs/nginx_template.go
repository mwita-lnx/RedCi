package jobs

import (
	"bytes"
	"text/template"
)

// RouteData feeds the nginx route template.
type RouteData struct {
	ID         int64
	Domain     string
	Aliases    []string // extra server_name hostnames
	Upstream   string   // simple mode: the single catch-all upstream
	SSL        bool
	Snippet    string
	HTTP2On    bool // nginx >= 1.25.1 uses `http2 on;`; older uses `listen ... http2`
	FrappeSite string
	StaticRoot string
	Locations  []RouteLocation // Model B path rules; empty = simple single-upstream mode
}

// ServerNames returns the space-joined server_name list (domain + aliases).
func (d RouteData) ServerNames() string {
	s := d.Domain
	for _, a := range d.Aliases {
		s += " " + a
	}
	return s
}

// tmplFuncs exposes the RouteAction/RouteMatch constants to the template so it
// can branch without magic strings.
var tmplFuncs = template.FuncMap{
	"isProxy":      func(a RouteAction) bool { return a == RouteActionProxy },
	"isFilesystem": func(a RouteAction) bool { return a == RouteActionFilesystem },
	"isRedirect":   func(a RouteAction) bool { return a == RouteActionRedirect },
	"locModifier": func(m RouteMatch) string {
		switch m {
		case RouteMatchExact:
			return "= "
		case RouteMatchRegex:
			return "~ "
		default:
			return ""
		}
	},
}

// routeTmpl renders one vhost file. The 443/80 scaffolding is shared; the body
// is either the simple catch-all proxy or the Model B location list.
//
// Websocket headers (Upgrade/Connection) are emitted ONLY on locations that opt
// in (simple mode's catch-all, or a Model B location with websocket:true). This
// is deliberate: forcing `Connection: upgrade` with an empty $http_upgrade makes
// Node/Next.js upstreams drop the connection (502). See NGINX report gotcha #4.
var routeTmpl = template.Must(template.New("route").Funcs(tmplFuncs).Parse(`# Managed by ops-panel. Do not edit by hand. route={{.ID}}
server {
    listen 80;
    listen [::]:80;
    server_name {{.ServerNames}};
{{- if .SSL }}
    location / { return 301 https://$host$request_uri; }
}

server {
{{- if .HTTP2On }}
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
{{- else }}
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
{{- end }}
    server_name {{.ServerNames}};
    ssl_certificate     /etc/letsencrypt/live/{{.Domain}}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/{{.Domain}}/privkey.pem;
{{- end }}
    client_max_body_size 50m;
{{- if .Locations }}
    # ---- Model B: path-split vhost ----
{{- range .Locations }}
    location {{ locModifier .Match }}{{ .Path }} {
{{- if isProxy .Action }}
        proxy_pass {{ .Target }};
        proxy_http_version 1.1;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
{{- if .FrappeProxy }}
        proxy_set_header X-Frappe-Site-Name {{ $.FrappeSite }};
{{- end }}
{{- if .Websocket }}
        proxy_set_header Upgrade           $http_upgrade;
        proxy_set_header Connection        $connection_upgrade;
{{- end }}
        proxy_read_timeout 120;
{{- else if isFilesystem .Action }}
        root {{ if .Target }}{{ .Target }}{{ else }}{{ $.StaticRoot }}{{ end }};
        try_files $uri =404;
        add_header Cache-Control "max-age=31536000";
{{- else if isRedirect .Action }}
        return 301 {{ .Target }};
{{- end }}
    }
{{- end }}
{{- else }}
    # ---- simple single-upstream proxy ----
    location / {
        proxy_pass {{.Upstream}};
        proxy_http_version 1.1;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade           $http_upgrade;
        proxy_set_header Connection        $connection_upgrade;
    }
{{- end }}
{{ .Snippet }}
}
`))

// RenderRoute renders the nginx config for a route.
func RenderRoute(d RouteData) (string, error) {
	var buf bytes.Buffer
	if err := routeTmpl.Execute(&buf, d); err != nil {
		return "", err
	}
	return buf.String(), nil
}
