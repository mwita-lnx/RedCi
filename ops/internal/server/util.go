package server

import (
	"database/sql"
	"strings"
)

// orDefault returns s, or def when s is empty.
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt(i int64) sql.NullInt64 {
	return sql.NullInt64{Int64: i, Valid: true}
}

func indexByte(s string, b byte) int { return strings.IndexByte(s, b) }
func trimSpace(s string) string      { return strings.TrimSpace(s) }
