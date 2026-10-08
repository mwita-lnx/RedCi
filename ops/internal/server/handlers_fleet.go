package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// apiInstallCommand mints a one-time enrollment token and returns the real
// `curl … | sudo sh` install one-liner for the Fleet page. Admin only.
func (s *Server) apiInstallCommand(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	_ = decodeJSON(w, r, &in)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = fmt.Sprintf("server-%d", time.Now().Unix())
	}
	id, token, err := s.CreateEnrollment(r.Context(), name, name)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	base := strings.TrimRight(s.cfg.BaseURL, "/")
	if base == "" {
		base = "http://" + r.Host
	}
	cmd := fmt.Sprintf("curl -fsSL %s/agent.sh | sudo sh -s -- --token %s", base, token)
	s.audit(r, "server.install_token", "server", id, name)
	writeJSONAPI(w, http.StatusOK, map[string]any{"id": id, "name": name, "command": cmd, "expires_in": "1 hour"})
}

// apiFleet powers the "Servers & agents" page: per-server cards with live
// metrics, fleet rollups, agent-version breakdown, and a derived attention list.
func (s *Server) apiFleet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	servers, _ := s.db.ReadQ.ListServers(ctx)
	now := time.Now().Unix()

	type card struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Hostname   string `json:"hostname"`
		Status     string `json:"status"`
		Online     bool   `json:"online"`
		Role       string `json:"role"`
		Agent      string `json:"agent_version"`
		CPUs       int    `json:"cpus"`
		MemTotalMB int64  `json:"mem_total_mb"`
		CPUPct     int    `json:"cpu_pct"`
		MemPct     int    `json:"mem_pct"`
		DiskPct    int    `json:"disk_pct"`
		OS         string `json:"os"`
		Benches    int    `json:"benches"`
		UptimeDays int64  `json:"uptime_days"`
		LastSeen   int64  `json:"last_seen"`
	}

	cards := make([]card, 0, len(servers))
	verCount := map[string]int{}
	online := 0
	var totalCPU int
	var totalMemMB, usedMemMB int64
	latest := ""

	for _, sv := range servers {
		var f protocol.Facts
		if sv.FactsJson.Valid {
			_ = json.Unmarshal([]byte(sv.FactsJson.String), &f)
		}
		benches, _ := s.db.ReadQ.ListBenchesForServer(ctx, sv.ID)
		isOnline := sv.Status == "online"
		if isOnline {
			online++
		}
		role := "runner"
		if len(benches) > 0 {
			role = "bench host"
		}
		ver := ""
		if sv.AgentVersion.Valid {
			ver = sv.AgentVersion.String
		}
		if ver != "" {
			verCount[ver]++
			if ver > latest {
				latest = ver
			}
		}
		totalCPU += f.CPUs
		totalMemMB += f.MemTotalMB
		usedMemMB += f.MemTotalMB * int64(f.MemUsedPct) / 100
		lastSeen := int64(0)
		if sv.LastHeartbeatAt.Valid {
			lastSeen = sv.LastHeartbeatAt.Int64
		}
		cards = append(cards, card{
			ID: sv.ID, Name: sv.Name, Hostname: sv.Hostname, Status: sv.Status,
			Online: isOnline, Role: role, Agent: ver,
			CPUs: f.CPUs, MemTotalMB: f.MemTotalMB,
			CPUPct: f.CPUUsedPct, MemPct: f.MemUsedPct, DiskPct: f.DiskUsedPct,
			OS: f.OS, Benches: len(benches),
			UptimeDays: (now - sv.CreatedAt) / 86400, LastSeen: lastSeen,
		})
	}

	// derived attention items
	type attn struct {
		Level  string `json:"level"` // warn | danger | info
		Server string `json:"server"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}
	attnList := make([]attn, 0)
	for _, c := range cards {
		if !c.Online {
			mins := (now - c.LastSeen) / 60
			attnList = append(attnList, attn{"danger", c.Name, c.Name + " is offline",
				detailOffline(mins)})
		} else if c.DiskPct >= 85 {
			attnList = append(attnList, attn{"warn", c.Name, c.Name + " disk at " + itoa(c.DiskPct) + "%",
				"Disk is nearly full — clean up old backups or logs."})
		}
		if latest != "" && c.Agent != "" && c.Agent < latest {
			attnList = append(attnList, attn{"info", c.Name, c.Name + " agent outdated",
				"Running " + c.Agent + " · latest is " + latest + "."})
		}
	}

	// agent-version breakdown, newest first
	type verRow struct {
		Version string `json:"version"`
		Count   int    `json:"count"`
		Latest  bool   `json:"latest"`
	}
	vers := make([]verRow, 0, len(verCount))
	for v, n := range verCount {
		vers = append(vers, verRow{v, n, v == latest})
	}
	sort.Slice(vers, func(i, j int) bool { return vers[i].Version > vers[j].Version })

	running, _ := s.db.ReadQ.CountRunningJobs(ctx)

	writeJSONAPI(w, http.StatusOK, map[string]any{
		"servers":        cards,
		"online":         online,
		"total":          len(cards),
		"total_cpu":      totalCPU,
		"total_mem_mb":   totalMemMB,
		"used_mem_mb":    usedMemMB,
		"running_jobs":   running,
		"agent_versions": vers,
		"latest_agent":   latest,
		"attention":      attnList,
		"enroll_hint":    "Add a server from this page to get a one-time enrollment command.",
	})
}

func detailOffline(mins int64) string {
	if mins <= 0 {
		return "No recent heartbeat."
	}
	return "No heartbeat for " + itoa(int(mins)) + " min."
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
