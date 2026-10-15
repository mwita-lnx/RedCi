package worker

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// TestPromoteTwoLockContention verifies that a job carrying two locks is only
// promoted when BOTH are free, and that a key held in either lock column of an
// active job blocks a contender on that key — the invariant the folded
// Press-style new_site job (bench lock + nginx lock) depends on.
func TestPromoteTwoLockContention(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	w := New(db, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	srv := mustServer(t, db, "s1")

	// Active job holding only the nginx lock (e.g. a running proxy-route job).
	seedJob(t, db, srv, "running", "nginx:1", "")

	// A queued new_site-style job that wants bench:1 AND nginx:1.
	wantBothFree := seedJob(t, db, srv, "queued", "bench:1", "nginx:1")

	// A queued job wanting only a free lock, to prove promotion still works.
	wantFree := seedJob(t, db, srv, "queued", "bench:2", "")

	if err := w.promote(ctx); err != nil {
		t.Fatalf("promote: %v", err)
	}

	// The two-lock job must stay queued: its nginx:1 lock is held via the
	// other job's lock_key column.
	if got := jobStatus(t, db, wantBothFree); got != "queued" {
		t.Errorf("two-lock job promoted despite held nginx lock: status=%q", got)
	}
	// The free-lock job must have been promoted.
	if got := jobStatus(t, db, wantFree); got != "ready" {
		t.Errorf("free-lock job not promoted: status=%q", got)
	}
}

// TestPromoteTwoLockReleased verifies the two-lock job promotes once both locks
// are free.
func TestPromoteTwoLockReleased(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t)
	w := New(db, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := mustServer(t, db, "s1")

	job := seedJob(t, db, srv, "queued", "bench:1", "nginx:1")
	if err := w.promote(ctx); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if got := jobStatus(t, db, job); got != "ready" {
		t.Fatalf("two-lock job with both locks free not promoted: status=%q", got)
	}
}

// --- helpers ---------------------------------------------------------------

func openMigratedDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mustServer(t *testing.T, db *store.DB, name string) int64 {
	t.Helper()
	s, err := db.WriteQ.CreateServer(context.Background(), store.CreateServerParams{
		Name: name, Hostname: name + ".local",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	return s.ID
}

// seedJob inserts a job directly with the given status and lock columns so the
// test controls contention precisely. Returns the job id.
func seedJob(t *testing.T, db *store.DB, serverID int64, status, lock1, lock2 string) int64 {
	t.Helper()
	res, err := db.Writer.ExecContext(context.Background(),
		`INSERT INTO jobs (server_id, type, params_json, status, lock_key, lock_key2, max_attempts)
		 VALUES (?, 'new_site', '{}', ?, ?, ?, 1)`,
		serverID, status, nullStr(lock1), nullStr(lock2),
	)
	if err != nil {
		t.Fatalf("seed job: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func jobStatus(t *testing.T, db *store.DB, id int64) string {
	t.Helper()
	var s string
	if err := db.Reader.QueryRowContext(context.Background(),
		`SELECT status FROM jobs WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatalf("read status: %v", err)
	}
	return s
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
