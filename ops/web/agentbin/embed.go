// Package agentbin embeds the prebuilt Linux agent binaries so the panel can
// serve the `curl … | sudo sh` install one-liner. The Dockerfile builds the
// real binaries into this directory before the panel is compiled; the
// .gitkeep placeholders keep a local `go build` working before a first build.
package agentbin

import "embed"

//go:embed ops-agent-linux-amd64 ops-agent-linux-arm64
var FS embed.FS
