// Package journal is the agent's local SQLite record of jobs in flight and an
// outbox of log/result payloads not yet accepted by the panel. On restart the
// agent reports any job still marked running as interrupted, then flushes the
// outbox, so a panel outage never loses output.
package journal

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	agentmigrations "github.com/mwita-lnx/RedCi/agent/db/agent/migrations"
)

// Status values stored in journal.status.
const (
	StatusRunning     = "running"
	StatusSucceeded   = "succeeded"
	StatusFailed      = "failed"
	StatusInterrupted = "interrupted"
)

// Journal wraps the agent's local database.
type Journal struct {
	db *sql.DB
}

// Open opens (and migrates) the agent journal at path.
func Open(path string) (*Journal, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	goose.SetBaseFS(agentmigrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		db.Close()
		return nil, err
	}
	if err := goose.Up(db, "."); err != nil {
		db.Close()
		return nil, fmt.Errorf("agent migrate: %w", err)
	}
	return &Journal{db: db}, nil
}

// Close closes the journal.
func (j *Journal) Close() error { return j.db.Close() }

// Begin records a job as running.
func (j *Journal) Begin(ctx context.Context, jobID int64, jobType, paramsJSON string) error {
	now := time.Now().Unix()
	_, err := j.db.ExecContext(ctx,
		`INSERT INTO journal (job_id, type, params_json, status, started_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (job_id) DO UPDATE SET status='running', updated_at=excluded.updated_at`,
		jobID, jobType, paramsJSON, StatusRunning, now, now)
	return err
}

// SetStep records the last step started for a job.
func (j *Journal) SetStep(ctx context.Context, jobID int64, step string) error {
	_, err := j.db.ExecContext(ctx,
		`UPDATE journal SET step=?, updated_at=? WHERE job_id=?`,
		step, time.Now().Unix(), jobID)
	return err
}

// Finish records a terminal status for a job.
func (j *Journal) Finish(ctx context.Context, jobID int64, status string) error {
	_, err := j.db.ExecContext(ctx,
		`UPDATE journal SET status=?, updated_at=? WHERE job_id=?`,
		status, time.Now().Unix(), jobID)
	return err
}

// MarkReported flags a job's result as accepted by the panel.
func (j *Journal) MarkReported(ctx context.Context, jobID int64) error {
	_, err := j.db.ExecContext(ctx, `UPDATE journal SET result_reported=1 WHERE job_id=?`, jobID)
	return err
}

// Interrupted is a job that was running when the agent last stopped.
type Interrupted struct {
	JobID int64
	Step  string
}

// FindInterrupted returns jobs still marked running (i.e. the agent crashed or
// rebooted mid-job) whose result has not been reported.
func (j *Journal) FindInterrupted(ctx context.Context) ([]Interrupted, error) {
	rows, err := j.db.QueryContext(ctx,
		`SELECT job_id, COALESCE(step,'') FROM journal WHERE status='running' AND result_reported=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Interrupted
	for rows.Next() {
		var it Interrupted
		if err := rows.Scan(&it.JobID, &it.Step); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// --- outbox ---

// Enqueue stores a payload (logs or result) to deliver to the panel.
func (j *Journal) Enqueue(ctx context.Context, jobID int64, kind, payload string) error {
	_, err := j.db.ExecContext(ctx,
		`INSERT INTO outbox (job_id, kind, payload) VALUES (?, ?, ?)`, jobID, kind, payload)
	return err
}

// OutboxItem is one queued payload.
type OutboxItem struct {
	ID      int64
	JobID   int64
	Kind    string
	Payload string
}

// PeekOutbox returns up to limit queued items in insertion order.
func (j *Journal) PeekOutbox(ctx context.Context, limit int) ([]OutboxItem, error) {
	rows, err := j.db.QueryContext(ctx,
		`SELECT id, job_id, kind, payload FROM outbox ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		if err := rows.Scan(&it.ID, &it.JobID, &it.Kind, &it.Payload); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// DeleteOutbox removes a delivered item.
func (j *Journal) DeleteOutbox(ctx context.Context, id int64) error {
	_, err := j.db.ExecContext(ctx, `DELETE FROM outbox WHERE id=?`, id)
	return err
}
