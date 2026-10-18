package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/uptrace/bun"
)

var (
	ErrNilDB              = errors.New("database: nil database")
	ErrTxOptionsMismatch  = errors.New("database: nested transaction options differ")
	ErrRollbackOnly       = errors.New("transaction marked rollback-only")
	ErrDifferentDatabase  = errors.New("transaction belongs to a different database")
	ErrNoTransaction      = errors.New("context has no transaction")
	ErrTransactionActive  = errors.New("transaction already active; use Execute to join it")
	ErrManagedTransaction = errors.New("Execute owns transaction completion")
)

type txKey struct{}

// Transaction contexts are single-owner; this state is not a concurrency primitive.
type txState struct {
	db      *bun.DB
	tx      bun.Tx
	failure error
	depth   int
	done    bool
	options sql.TxOptions
}

func (s *txState) fail(err error) { s.failure = errors.Join(s.failure, err) }

// TxManager can be shared across requests. Each transaction context is owned by
// a single execution and must not be shared concurrently between goroutines.
type TxManager struct {
	db      *bun.DB
	options *sql.TxOptions
}

// NewTxManager constructs a transaction manager using a caller-owned database.
// Options are copied. Nil options join the enclosing transaction's options;
// explicit options must match when joining an existing scope.
func NewTxManager(db *bun.DB, options *sql.TxOptions) (*TxManager, error) {
	if db == nil {
		return nil, ErrNilDB
	}
	manager := &TxManager{db: db}
	if options != nil {
		if options.Isolation < sql.LevelDefault || options.Isolation > sql.LevelLinearizable {
			return nil, errors.New("database: invalid transaction isolation")
		}
		copy := *options
		manager.options = &copy
	}
	return manager, nil
}

// TxFromContext returns the active Bun transaction for this database. This is
// useful for adapters accepting its underlying *sql.Tx; it does not transfer
// ownership of Commit or Rollback.
func TxFromContext(ctx context.Context, db *bun.DB) (bun.Tx, error) {
	state, err := stateFor(ctx, db)
	if err != nil {
		return bun.Tx{}, err
	}
	return state.tx, nil
}

func stateFor(ctx context.Context, db *bun.DB) (*txState, error) {
	state, ok := ctx.Value(txKey{}).(*txState)
	if db == nil {
		return nil, ErrNilDB
	}
	if !ok {
		return nil, ErrNoTransaction
	}
	if state.db != db {
		state.fail(ErrDifferentDatabase)
		return nil, ErrDifferentDatabase
	}
	if state.done {
		return nil, sql.ErrTxDone
	}
	return state, nil
}

// RequireNoTransaction rejects a transaction-bearing context before an operation
// performs nontransactional effects such as mail, queue submission or blob writes.
// Use transactional intent persistence when effects must follow a database commit.
func RequireNoTransaction(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if state, ok := ctx.Value(txKey{}).(*txState); ok {
		if state.done {
			return sql.ErrTxDone
		}
		return ErrTransactionActive
	}
	return nil
}

// Executor ensures participating repositories use the active transaction.
func Executor(ctx context.Context, db *bun.DB) (bun.IDB, error) {
	state, err := stateFor(ctx, db)
	if errors.Is(err, ErrNoTransaction) {
		return db, nil
	}
	if err != nil {
		return nil, err
	}
	return state.tx, nil
}

// Begin rejects an already active scope; it does not create savepoints.
func (m *TxManager) Begin(ctx context.Context) (context.Context, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := ctx.Value(txKey{}).(*txState); ok {
		if _, err := stateFor(ctx, m.db); err != nil {
			return nil, err
		}
		return nil, ErrTransactionActive
	}
	if m.db == nil {
		return nil, ErrNilDB
	}
	tx, err := m.db.BeginTx(ctx, m.options)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	state := &txState{db: m.db, tx: tx}
	if m.options != nil {
		state.options = *m.options
	}
	return context.WithValue(ctx, txKey{}, state), nil
}
func rollback(state *txState) error {
	if state.done {
		return nil
	}
	state.done = true
	err := state.tx.Rollback()
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return err
}
func (m *TxManager) Commit(ctx context.Context) error {
	state, err := stateFor(ctx, m.db)
	if err != nil {
		return err
	}
	if state.depth > 0 {
		state.fail(ErrManagedTransaction)
		return ErrManagedTransaction
	}
	if state.failure != nil {
		return errors.Join(ErrRollbackOnly, state.failure, rollback(state))
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, rollback(state))
	}
	state.done = true
	if err := state.tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction (outcome may be uncertain): %w", err)
	}
	return nil
}

// Rollback remains usable after cancellation and is idempotent after completion.
func (m *TxManager) Rollback(ctx context.Context) error {
	state, err := stateFor(ctx, m.db)
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.depth > 0 {
		state.fail(ErrManagedTransaction)
		return ErrManagedTransaction
	}
	return rollback(state)
}

// Execute joins an existing scope. Nested errors/panics make the outer scope
// rollback-only, even when caught. Only the scope that began the transaction ends it.
func (m *TxManager) Execute(ctx context.Context, fn func(context.Context) error) (err error) {
	if fn == nil {
		err = errors.New("transaction callback is required")
	} else {
		err = ctx.Err()
	}
	if err != nil {
		if state, ok := ctx.Value(txKey{}).(*txState); ok && !state.done {
			state.fail(err)
		}
		return err
	}
	_, nested := ctx.Value(txKey{}).(*txState)
	if !nested {
		ctx, err = m.Begin(ctx)
		if err != nil {
			return err
		}
	}
	state, err := stateFor(ctx, m.db)
	if err != nil {
		return err
	}
	if nested && m.options != nil && *m.options != state.options {
		state.fail(ErrTxOptionsMismatch)
		return ErrTxOptionsMismatch
	}
	state.depth++
	completed := false
	defer func() {
		state.depth--
		if !completed {
			state.fail(errors.New("transaction callback panicked"))
		}
		if err != nil {
			state.fail(err)
		}
		if !nested {
			if !completed || err != nil {
				err = errors.Join(err, rollback(state))
			} else {
				err = m.Commit(ctx)
			}
		}
	}()
	err = fn(ctx)
	if err == nil {
		err = ctx.Err()
	}
	completed = true
	return err
}

// Savepoint runs fn in an isolated scope of the active transaction. It rolls back
// that scope on error or panic; successful work still depends on the outer commit.
// Unlike Execute's nested scopes, a rolled-back Savepoint does not mark its parent
// rollback-only unless savepoint cleanup fails. The caller must handle its error.
// An active transaction is required. The database must support SQL savepoints.
func (m *TxManager) Savepoint(ctx context.Context, fn func(context.Context) error) (err error) {
	if fn == nil {
		return errors.New("transaction callback is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := stateFor(ctx, m.db)
	if err != nil {
		return err
	}
	if parent.failure != nil {
		return errors.Join(ErrRollbackOnly, parent.failure)
	}
	tx, err := parent.tx.BeginTx(ctx, nil)
	if err != nil {
		parent.fail(err)
		return err
	}
	state := &txState{db: m.db, tx: tx, options: parent.options, depth: 1}
	released := false
	defer func() {
		state.done = true
		if !released {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				parent.fail(rollbackErr)
				err = errors.Join(err, rollbackErr)
			}
		}
	}()
	err = fn(context.WithValue(ctx, txKey{}, state))
	if state.failure != nil {
		err = errors.Join(err, ErrRollbackOnly, state.failure)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		parent.fail(err)
		return err
	}
	released = true
	return nil
}
