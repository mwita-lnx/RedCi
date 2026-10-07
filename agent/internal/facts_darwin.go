package agentcfg

import (
	"os/exec"
	"strconv"
	"strings"
)

// loadAvg1 reads the 1-minute load average via `sysctl -n vm.loadavg`.
func loadAvg1() (float64, bool) {
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return 0, false
	}
	// output like: { 2.14 2.30 2.41 }
	fields := strings.Fields(strings.Trim(strings.TrimSpace(string(out)), "{} "))
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// memInfo returns total MB and a used% estimate from sysctl + vm_stat.
func memInfo() (totalMB int64, usedPct int, ok bool) {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, 0, false
	}
	totalBytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || totalBytes == 0 {
		return 0, 0, false
	}
	totalMB = totalBytes / (1 << 20)

	// vm_stat: estimate free (free + inactive) pages against total.
	vs, err := exec.Command("vm_stat").Output()
	if err != nil {
		return totalMB, 0, true
	}
	pageSize := int64(4096)
	var freePages, inactivePages int64
	for _, line := range strings.Split(string(vs), "\n") {
		parse := func() int64 {
			p := strings.TrimSpace(strings.TrimRight(line[strings.Index(line, ":")+1:], "."))
			n, _ := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
			return n
		}
		switch {
		case strings.HasPrefix(line, "Pages free:"):
			freePages = parse()
		case strings.HasPrefix(line, "Pages inactive:"):
			inactivePages = parse()
		}
	}
	freeBytes := (freePages + inactivePages) * pageSize
	used := totalBytes - freeBytes
	if used < 0 {
		used = 0
	}
	return totalMB, int(used * 100 / totalBytes), true
}
