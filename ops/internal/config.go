// Package panel holds panel-wide configuration shared by the CLI and server.
package ops

import (
	"os"
)

// Config is the panel's runtime configuration, from environment variables.
// A TOML file can be layered on later; env is enough for Phase 0.
type Config struct {
	Listen        string // OPS_LISTEN, e.g. 127.0.0.1:8080
	DBPath        string // OPS_DB
	BaseURL       string // OPS_BASE_URL
	MasterKeyPath string // OPS_MASTER_KEY (fallback when CREDENTIALS_DIRECTORY unset)
	Dev           bool   // OPS_DEV: relaxes cookie Secure flag for plain-HTTP local dev
	AlertWebhook  string // OPS_ALERT_WEBHOOK: Slack-compatible outgoing webhook URL
}

// LoadConfig reads configuration from the environment, applying defaults.
func LoadConfig() Config {
	return Config{
		Listen:        envOr("OPS_LISTEN", "127.0.0.1:8080"),
		DBPath:        envOr("OPS_DB", "dev.db"),
		BaseURL:       envOr("OPS_BASE_URL", "http://127.0.0.1:8080"),
		MasterKeyPath: envOr("OPS_MASTER_KEY", ""),
		Dev:           os.Getenv("OPS_DEV") == "1",
		AlertWebhook:  envOr("OPS_ALERT_WEBHOOK", ""),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
