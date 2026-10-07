// Package agentcfg loads the agent's configuration from its TOML file and
// environment, and manages the on-disk credential written at enrollment.
package agentcfg

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the agent's static configuration from /etc/ops-agent/config.toml
// (or $OPS_AGENT_CONFIG). Environment variables override the file so the same
// image can run under Docker with nothing but env set.
type Config struct {
	PanelURL     string   `toml:"panel_url"`
	BenchRoots   []string `toml:"bench_roots"`
	AppsRoot     string   `toml:"apps_root"`
	BenchPathEnv string   `toml:"bench_path_env"`
	LogLevel     string   `toml:"log_level"`
	CAFile       string   `toml:"ca_file"`          // optional pinned private CA
	CredFile     string   `toml:"credential_file"`  // where the enrollment credential lives
}

// Credential is written after a successful enrollment and read on every start.
type Credential struct {
	ServerID int64  `json:"server_id"`
	Token    string `json:"token"`
}

// Load reads the config file (if present) and applies env overrides.
func Load() (Config, error) {
	c := Config{
		AppsRoot:     "/srv/ops/apps",
		LogLevel:     "info",
		CredFile:     "/etc/ops-agent/credential",
		BenchPathEnv: "/home/frappe/.local/bin",
	}
	path := envOr("OPS_AGENT_CONFIG", "/etc/ops-agent/config.toml")
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &c); err != nil {
			return c, fmt.Errorf("agent config %s: %w", path, err)
		}
	}
	if v := os.Getenv("OPS_PANEL_URL"); v != "" {
		c.PanelURL = v
	}
	if v := os.Getenv("OPS_AGENT_BENCH_ROOTS"); v != "" {
		c.BenchRoots = strings.Split(v, ",")
	}
	if v := os.Getenv("OPS_AGENT_BENCH_PATH_ENV"); v != "" {
		c.BenchPathEnv = v
	}
	if v := os.Getenv("OPS_AGENT_APPS_ROOT"); v != "" {
		c.AppsRoot = v
	}
	if v := os.Getenv("OPS_AGENT_CREDENTIAL"); v != "" {
		c.CredFile = v
	}
	if v := os.Getenv("OPS_AGENT_CA_FILE"); v != "" {
		c.CAFile = v
	}
	if v := os.Getenv("OPS_AGENT_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	return c, nil
}

// LoadCredential reads the stored enrollment credential.
func (c Config) LoadCredential() (Credential, error) {
	var cred Credential
	raw, err := os.ReadFile(c.CredFile)
	if err != nil {
		return cred, fmt.Errorf("read credential %s: %w", c.CredFile, err)
	}
	if err := json.Unmarshal(raw, &cred); err != nil {
		return cred, fmt.Errorf("parse credential: %w", err)
	}
	return cred, nil
}

// SaveCredential writes the credential with 0600 permissions.
func (c Config) SaveCredential(cred Credential) error {
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	return os.WriteFile(c.CredFile, raw, 0o600)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
