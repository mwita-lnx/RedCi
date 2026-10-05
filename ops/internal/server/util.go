package server

import (
	"database/sql"
	"strings"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// listAuditDefault returns params for the most recent 100 audit rows.
func listAuditDefault() store.ListAuditParams {
	return store.ListAuditParams{Limit: 100, Offset: 0}
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
