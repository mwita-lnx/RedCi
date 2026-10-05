package jobs

import (
	"bytes"
	"text/template"
)

// RouteData feeds the nginx route template.
type RouteData struct {
	ID       int64
	Domain   string
	Upstream string
	SSL      bool
	Snippet  string
	HTTP2On  bool // nginx >= 1.25.1 uses `http2 on;`; older uses `listen ... http2`
}

// routeTmpl is the embedded nginx route template (one file per route).
var routeTmpl = template.Must(template.New("route").Parse(`# Managed by ops-panel. Do not edit by hand. route={{.ID}}
server {
    listen 80;
    listen [::]:80;
    server_name {{.Domain}};
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
    server_name {{.Domain}};
    ssl_certificate     /etc/letsencrypt/live/{{.Domain}}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/{{.Domain}}/privkey.pem;
{{- end }}
    client_max_body_size 50m;

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
