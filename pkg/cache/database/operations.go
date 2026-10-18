package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun/dialect"
	"github.com/wisalgroup/labena/pkg/cache"
	dbconn "github.com/wisalgroup/labena/pkg/database"
)

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := cache.ValidateKey(key); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	exec, err := dbconn.Executor(ctx, s.db)
	if err != nil {
		return false, err
	}
	var found bool
	err = exec.QueryRowContext(ctx, fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE namespace=?0 AND cache_key=?1 AND expires_at>%s)", s.table, s.now), s.cfg.Namespace, key).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("cache/database: exists: %w", err)
	}
	return found, nil
}

// Clear deletes this namespace through the caller's transaction executor.
// It does not drop tables or close the shared database pool.
func (s *Store) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	exec, err := dbconn.Executor(ctx, s.db)
	if err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, "DELETE FROM "+s.table+" WHERE namespace=?0", s.cfg.Namespace); err != nil {
		return fmt.Errorf("cache/database: clear: %w", err)
	}
	return nil
}

func (s *Store) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cache.ValidateKeys(keys); err != nil {
		return nil, err
	}
	result := make(map[string][]byte, len(keys))
	if len(keys) == 0 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OperationTimeout)
	defer cancel()
	exec, err := dbconn.Executor(ctx, s.db)
	if err != nil {
		return nil, err
	}
	args := []any{s.cfg.Namespace}
	placeholders := make([]string, len(keys))
	for i, key := range keys {
		placeholders[i] = fmt.Sprintf("?%d", len(args))
		args = append(args, key)
	}
	rows, err := exec.QueryContext(ctx, fmt.Sprintf("SELECT cache_key,value FROM %s WHERE namespace=?0 AND cache_key IN(%s) AND expires_at>%s", s.table, strings.Join(placeholders, ","), s.now), args...)
	if err != nil {
		return nil, fmt.Errorf("cache/database: get multi: %w", err)
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("cache/database: get multi: %w", err)
		}
		total += len(key) + len(value)
		if total > cache.MaxBatchBytes {
			return nil, cache.ErrTooLarge
		}
		result[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cache/database: get multi: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("cache/database: get multi: %w", err)
	}
	return result, nil
}

func (s *Store) SetMulti(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	keys, err := cache.ValidateItems(ctx, items, ttl, s.cfg.MaxEntryBytes)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
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
	args := []any{millis, s.cfg.Namespace}
	values := make([]string, 0, len(keys))
	expiry := strings.ReplaceAll(s.expiry, "?3", "?0")
	for _, key := range keys {
		values = append(values, fmt.Sprintf("(?1,?%d,?%d,%s)", len(args), len(args)+1, expiry))
		value := items[key]
		if value == nil {
			value = []byte{}
		}
		args = append(args, key, value)
	}
	query := "INSERT INTO " + s.table + "(namespace,cache_key,value,expires_at) VALUES" + strings.Join(values, ",")
	if s.db.Dialect().Name() == dialect.PG {
		query += " ON CONFLICT(namespace,cache_key) DO UPDATE SET value=EXCLUDED.value,expires_at=EXCLUDED.expires_at"
	} else {
		query += " AS incoming ON DUPLICATE KEY UPDATE value=incoming.value,expires_at=incoming.expires_at"
	}
	if _, err := exec.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("cache/database: set multi: %w", err)
	}
	return nil
}
