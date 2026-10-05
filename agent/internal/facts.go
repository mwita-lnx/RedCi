package agentcfg

import (
	"os"
	"runtime"

	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// CollectFacts gathers lightweight host facts for register/heartbeat. Deep
// probes (nginx/bench/docker versions, disk) are filled in by the server_status
// job; this is the cheap subset sent on every heartbeat.
func (c Config) CollectFacts() protocol.Facts {
	f := protocol.Facts{
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		CPUs:       runtime.NumCPU(),
		BenchRoots: c.BenchRoots,
	}
	return f
}

// Hostname returns the host's name, falling back to "unknown".
func Hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "unknown"
}
