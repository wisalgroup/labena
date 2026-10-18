package repository

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/pkg/database"
)

func executor(ctx context.Context, pool *bun.DB) (bun.IDB, error) {
	if pool == nil {
		return nil, database.ErrNilDB
	}
	return database.Executor(ctx, pool)
}

// authenticationTransaction owns the commit. Enclosing transactions are rejected
// so a caller cannot roll back failed-code attempts or refresh-replay revocation.
// Every participating helper must use the supplied executor, never the pool.
func authenticationTransaction(ctx context.Context, pool *bun.DB, fn func(context.Context, bun.IDB) error) error {
	_, err := database.TxFromContext(ctx, pool)
	if err == nil {
		return database.ErrTransactionActive
	}
	if !errors.Is(err, database.ErrNoTransaction) {
		return err
	}
	tx, err := database.NewTxManager(pool, nil)
	if err != nil {
		return err
	}
	return tx.Execute(ctx, func(ctx context.Context) error {
		db, err := executor(ctx, pool)
		if err != nil {
			return err
		}
		return fn(ctx, db)
	})
}
