package agentcfg

import (
	"os"
	"strconv"
	"strings"
)

// loadAvg1 reads the 1-minute load average from /proc/loadavg.
func loadAvg1() (float64, bool) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// memInfo reads total MB and used% from /proc/meminfo.
func memInfo() (totalMB int64, usedPct int, ok bool) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	var totalKB, availKB int64
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			totalKB = v
		case "MemAvailable:":
			availKB = v
		}
	}
	if totalKB == 0 {
		return 0, 0, false
	}
	used := totalKB - availKB
	return totalKB / 1024, int(used * 100 / totalKB), true
}
