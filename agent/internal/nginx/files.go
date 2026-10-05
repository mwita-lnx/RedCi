package nginx

import (
	"os"
	"strings"
)

// readIfExists reads a file, reporting whether it existed.
func readIfExists(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return b, true
}

// writeFileSync writes data and fsyncs it to disk before returning, so a
// following rename gives a crash-safe atomic swap.
func writeFileSync(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// containsConflict reports whether nginx -t output warns about a conflicting
// server name, which we treat as a failure.
func containsConflict(out string) bool {
	return strings.Contains(out, "conflicting server name")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
