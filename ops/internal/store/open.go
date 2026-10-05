package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	migrations "github.com/mwita-lnx/RedCi/ops/db/panel/migrations"
)

// DB bundles the two connection pools the panel uses against one SQLite file:
// a single-connection Writer (so writes never collide and "database is locked"
// never appears) and a multi-connection Reader pool that runs alongside it
// under WAL. Both carry a *Queries for sqlc-generated access.
type DB struct {
	Writer  *sql.DB
	Reader  *sql.DB
	WriteQ  *Queries
	ReadQ   *Queries
	path    string
}

// dsn builds the connection string with the pragmas the spec requires.
// _txlock=immediate makes every transaction take the write lock up front,
// which keeps the single writer honest.
func dsn(path string) string {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	return "file:" + path + "?" + q.Encode()
}

// Open returns a DB with a 1-connection writer and a reader pool against path.
func Open(path string) (*DB, error) {
	writer, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	writer.SetMaxOpenConns(1)

	reader, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		writer.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	reader.SetMaxOpenConns(8)
	reader.SetMaxIdleConns(8)

	db := &DB{
		Writer: writer,
		Reader: reader,
		WriteQ: New(writer),
		ReadQ:  New(reader),
		path:   path,
	}
	if err := db.Writer.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return db, nil
}

// Migrate runs all embedded goose migrations to the latest version.
// It runs on the writer pool so it holds the single write connection.
func (db *DB) Migrate(ctx context.Context) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db.Writer, "."); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Close shuts down both pools.
func (db *DB) Close() error {
	var first error
	if db.Reader != nil {
		if err := db.Reader.Close(); err != nil {
			first = err
		}
	}
	if db.Writer != nil {
		if err := db.Writer.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
