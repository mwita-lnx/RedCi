package agentcfg

import (
	"os"
	"runtime"
	"syscall"

	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// CollectFacts gathers lightweight host facts for register/heartbeat. Deep
// probes (nginx/bench/docker versions) are filled in by the server_status job;
// this is the cheap subset sent on every heartbeat, now including live
// CPU/memory/disk utilization for the fleet view.
func (c Config) CollectFacts() protocol.Facts {
	cpus := runtime.NumCPU()
	f := protocol.Facts{
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		CPUs:       cpus,
		BenchRoots: c.BenchRoots,
	}
	if load1, ok := loadAvg1(); ok && cpus > 0 {
		pct := int(load1 / float64(cpus) * 100)
		if pct > 100 {
			pct = 100
		}
		f.CPUUsedPct = pct
	}
	if totalMB, usedPct, ok := memInfo(); ok {
		f.MemTotalMB = totalMB
		f.MemUsedPct = usedPct
	}
	root := "/"
	if len(c.BenchRoots) > 0 {
		root = c.BenchRoots[0]
	}
	if usedPct, freeGB, ok := diskInfo(root); ok {
		f.DiskUsedPct = usedPct
		f.DiskFreeGB = freeGB
	}
	return f
}

// diskInfo returns disk used% and free GB for the filesystem containing path.
func diskInfo(path string) (usedPct int, freeGB int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	if total == 0 {
		return 0, 0, false
	}
	used := total - free
	return int(used * 100 / total), int64(free / (1 << 30)), true
}

// Hostname returns the host's name, falling back to "unknown".
func Hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "unknown"
}
