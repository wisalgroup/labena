// Package database caches bytes in PostgreSQL or MySQL through a caller-owned Bun DB.
// Apply the central cache_entries migration before use; constructors never run DDL.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"

	"github.com/wisalgroup/labena/pkg/cache"
	dbconn "github.com/wisalgroup/labena/pkg/database"
)

type Config struct {
	Table            string        // default cache_entries; bare lowercase SQL identifier
	Namespace        string        // default default; separates stores sharing a table
	MaxEntryBytes    int           // key + value bytes on Set; default 1 MiB
	OperationTimeout time.Duration // default 5 seconds
}

// withDefaults resolves zero settings without changing the caller's value.
func (c Config) withDefaults() Config {
	if c.Table == "" {
		c.Table = "cache_entries"
	}
	if c.Namespace == "" {
		c.Namespace = "default"
	}
	if c.MaxEntryBytes == 0 {
		c.MaxEntryBytes = 1 << 20
	}
	if c.OperationTimeout == 0 {
		c.OperationTimeout = 5 * time.Second
	}
	return c
}

// Validate checks settings without I/O. Zero fields use the documented defaults.
func (c Config) Validate() error {
	c = c.withDefaults()

	if !tableName.MatchString(c.Table) || cache.ValidateKey(c.Namespace) != nil || strings.Contains(c.Namespace, ":") || c.MaxEntryBytes < 1 || c.OperationTimeout <= 0 {
		return errors.New("cache/database: invalid table, namespace, size limit or timeout")
	}
	return nil
}

// Store borrows DB and never closes it. Cache operations join the transaction
// in ctx through pkg/database.Executor. Construct once; safe for concurrent use.
type Store struct {
	db                 *bun.DB
	cfg                Config
	table, now, expiry string
}

var tableName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

func New(db *bun.DB, cfg Config) (*Store, error) {
	if db == nil || db.DB == nil || (db.Dialect().Name() != dialect.PG && db.Dialect().Name() != dialect.MySQL) {
		return nil, errors.New("cache/database: PostgreSQL or MySQL Bun database required")
	}
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	quote := string(db.Dialect().IdentQuote())
	now, expiry := "clock_timestamp()", "clock_timestamp()+?3*interval '1 millisecond'"
	if db.Dialect().Name() == dialect.MySQL {
		now, expiry = "UTC_TIMESTAMP(6)", "TIMESTAMPADD(MICROSECOND,?3*1000,UTC_TIMESTAMP(6))"
	}
	return &Store{db: db, cfg: cfg, table: quote + cfg.Table + quote, now: now, expiry: expiry}, nil
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := cache.ValidateKey(key); err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	exec, err := dbconn.Executor(ctx, s.db)
	if err != nil {
		return nil, false, err
	}
	var value []byte
	err = exec.QueryRowContext(ctx, fmt.Sprintf("SELECT value FROM %s WHERE namespace=?0 AND cache_key=?1 AND expires_at>%s", s.table, s.now), s.cfg.Namespace, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache/database: get: %w", err)
	}
	return value, true, nil
}

func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cache.ValidateKey(key); err != nil {
		return err
	}
	if ttl <= 0 {
		return cache.ErrInvalidTTL
	}
	if len(value) > s.cfg.MaxEntryBytes-len(key) {
		return cache.ErrTooLarge
	}
	if value == nil {
		value = []byte{}
	}
	millis := ttl.Milliseconds()
	if ttl%time.Millisecond != 0 {
		millis++
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	exec, err := dbconn.Executor(ctx, s.db)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("INSERT INTO %s(namespace,cache_key,value,expires_at) VALUES(?0,?1,?2,%s)", s.table, s.expiry)
	if s.db.Dialect().Name() == dialect.PG {
		query += " ON CONFLICT(namespace,cache_key) DO UPDATE SET value=EXCLUDED.value,expires_at=EXCLUDED.expires_at"
	} else {
		query += " ON DUPLICATE KEY UPDATE value=?2,expires_at=" + s.expiry
	}
	if _, err = exec.ExecContext(ctx, query, s.cfg.Namespace, key, value, millis); err != nil {
		return fmt.Errorf("cache/database: set: %w", err)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cache.ValidateKey(key); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	exec, err := dbconn.Executor(ctx, s.db)
	if err != nil {
		return err
	}
	if _, err = exec.ExecContext(ctx, "DELETE FROM "+s.table+" WHERE namespace=?0 AND cache_key=?1", s.cfg.Namespace, key); err != nil {
		return fmt.Errorf("cache/database: delete: %w", err)
	}
	return nil
}

// PruneExpired deletes at most limit expired rows in this namespace (1–10000).
// Call periodically outside business transactions. It uses a separate, bounded
// READ COMMITTED transaction, skips locked rows and never deletes a refreshed row.
// Expired values are already misses even if this cleanup has not run.
func (s *Store) PruneExpired(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 10000 {
		return 0, errors.New("cache/database: prune limit must be between 1 and 10000")
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT cache_key FROM %s WHERE namespace=?0 AND expires_at<=%s ORDER BY expires_at,cache_key LIMIT ?1 FOR UPDATE SKIP LOCKED", s.table, s.now), s.cfg.Namespace, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	args := []any{s.cfg.Namespace}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return 0, err
		}
		keys = append(keys, fmt.Sprintf("?%d", len(args)))
		args = append(args, key)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}
	result, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE namespace=?0 AND cache_key IN(%s)", s.table, strings.Join(keys, ",")), args...)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	committed = true
	return count, nil
}

var _ cache.Store = (*Store)(nil)

// Remember returns a cached JSON value or loads it on a miss. See cache.Remember
// for validation, error, expiry and concurrent-loading semantics.
func (s *Store) Remember(ctx context.Context, key string, ttl time.Duration, load func(context.Context) (any, error)) (any, error) {
	return cache.Remember(ctx, s, key, ttl, load)
}
