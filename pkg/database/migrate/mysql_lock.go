package migrate

import (
	"context"
	"database/sql"
	"errors"
)

// MySQL named locks belong to the migration session, survive DDL's implicit
// commits and release when the connection closes. Hash the database name so
// independent databases can migrate concurrently without another lock table.
type mysqlLocker struct{ name string }

const mysqlLockName = "CONCAT('labena:',LEFT(SHA2(DATABASE(),256),48))"

func (l mysqlLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var acquired sql.NullInt64
		if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK("+l.expression()+",1)", l.args()...).Scan(&acquired); err != nil {
			return err
		}
		if !acquired.Valid {
			return errors.New("migrations: MySQL lock unavailable")
		}
		if acquired.Int64 == 1 {
			return nil
		}
	}
}
func (l mysqlLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	var released sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK("+l.expression()+")", l.args()...).Scan(&released); err != nil {
		return err
	}
	if !released.Valid || released.Int64 != 1 {
		return errors.New("migrations: MySQL lock not owned")
	}
	return nil
}

func (l mysqlLocker) expression() string {
	if l.name != "" {
		return "?"
	}
	return mysqlLockName
}
func (l mysqlLocker) args() []any {
	if l.name != "" {
		return []any{l.name}
	}
	return nil
}
