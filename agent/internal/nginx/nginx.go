// Package nginx owns /etc/nginx/ops.d: it renders a route's config, swaps it
// atomically, validates with `nginx -t`, and reloads — restoring the previous
// file if validation fails. It never touches bench's generated config.
package nginx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mwita-lnx/RedCi/agent/internal/runner"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// Dir is the directory the agent owns, included from nginx.conf.
const Dir = "/etc/nginx/ops.d"

// BackupDir holds the last few versions of each route config.
const BackupDir = "/var/lib/ops-agent/nginx-backups"

// Manager applies nginx route changes.
type Manager struct {
	run     *runner.Runner
	dir     string
	backups string
}

// New builds a Manager. dir/backups default to the standard paths when empty.
func New(run *runner.Runner, dir, backups string) *Manager {
	if dir == "" {
		dir = Dir
	}
	if backups == "" {
		backups = BackupDir
	}
	return &Manager{run: run, dir: dir, backups: backups}
}

func (m *Manager) routePath(domain string) string {
	return filepath.Join(m.dir, domain+".conf")
}

// Apply renders and installs a route config. On nginx -t failure it restores
// the previous file (or removes the new one) and returns the error with the
// nginx -t output. On success it reloads nginx and returns the config's sha256.
func (m *Manager) Apply(ctx context.Context, content, domain string, log func(stream, line string)) (hash string, err error) {
	if err := os.MkdirAll(m.backups, 0o750); err != nil {
		return "", err
	}
	target := m.routePath(domain)

	// 1. Back up the current file, if any.
	prev, hadPrev := readIfExists(target)
	if hadPrev {
		bpath := filepath.Join(m.backups, fmt.Sprintf("%s.%d.conf", domain, time.Now().Unix()))
		_ = os.WriteFile(bpath, prev, 0o644)
		m.pruneBackups(domain, 10)
	}

	// 2. Write to a temp file WITHOUT a .conf suffix (nginx never loads it),
	//    fsync, then rename over the real file — the swap is atomic.
	tmp := filepath.Join(m.dir, "."+domain+".tmp")
	if err := writeFileSync(tmp, []byte(content), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	log("system", "wrote "+target)

	// 3. nginx -t. Treat a conflicting server name warning as a failure.
	out, _, terr := m.run.Exec(ctx, jobs.Step{
		Command: "sudo", Args: []string{"/usr/sbin/nginx", "-t"},
	}, time.Minute)
	if terr != nil || containsConflict(out) {
		log("stderr", out)
		// 5. Restore the backup (or delete the new file if there was none).
		if hadPrev {
			_ = writeFileSync(target, prev, 0o644)
		} else {
			_ = os.Remove(target)
		}
		// Confirm the restore is valid.
		if _, _, rerr := m.run.Exec(ctx, jobs.Step{Command: "sudo", Args: []string{"/usr/sbin/nginx", "-t"}}, time.Minute); rerr != nil {
			log("stderr", "restore left nginx -t failing; manual intervention needed")
		}
		return "", fmt.Errorf("nginx -t failed: %s", firstLine(out))
	}

	// 6. Reload.
	if _, _, rerr := m.run.Exec(ctx, jobs.Step{
		Command: "sudo", Args: []string{"/usr/bin/systemctl", "reload", "nginx"},
	}, time.Minute); rerr != nil {
		return "", fmt.Errorf("nginx reload failed: %w", rerr)
	}
	log("system", "nginx reloaded")

	// 7. Return the config hash.
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:]), nil
}

// Delete removes a route config, validates, and reloads (restoring on failure).
func (m *Manager) Delete(ctx context.Context, domain string, log func(stream, line string)) error {
	target := m.routePath(domain)
	prev, had := readIfExists(target)
	if !had {
		return nil // already gone
	}
	if err := os.Remove(target); err != nil {
		return err
	}
	if _, _, terr := m.run.Exec(ctx, jobs.Step{Command: "sudo", Args: []string{"/usr/sbin/nginx", "-t"}}, time.Minute); terr != nil {
		_ = writeFileSync(target, prev, 0o644) // restore
		return fmt.Errorf("nginx -t failed after delete; restored: %w", terr)
	}
	if _, _, rerr := m.run.Exec(ctx, jobs.Step{Command: "sudo", Args: []string{"/usr/bin/systemctl", "reload", "nginx"}}, time.Minute); rerr != nil {
		return fmt.Errorf("nginx reload failed: %w", rerr)
	}
	log("system", "removed "+target+" and reloaded")
	return nil
}

// ConfigFile is one nginx file returned by a config listing.
type ConfigFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

// ListOpsConfigs returns every file under the ops.d directory with its sha256,
// for drift detection on the panel.
func (m *Manager) ListOpsConfigs() ([]ConfigFile, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []ConfigFile
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".conf" {
			continue
		}
		p := filepath.Join(m.dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(b)
		out = append(out, ConfigFile{Path: p, Content: string(b), SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// pruneBackups keeps only the newest keep backups for a domain.
func (m *Manager) pruneBackups(domain string, keep int) {
	entries, err := os.ReadDir(m.backups)
	if err != nil {
		return
	}
	var matches []string
	for _, e := range entries {
		name := e.Name()
		if len(name) > len(domain) && name[:len(domain)+1] == domain+"." {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches) // unix-ts in the name sorts chronologically
	for i := 0; i < len(matches)-keep; i++ {
		_ = os.Remove(filepath.Join(m.backups, matches[i]))
	}
}
