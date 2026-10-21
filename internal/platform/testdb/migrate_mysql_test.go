package testdb_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func TestMySQLMigrationConcurrencyAndLock(t *testing.T) {
	db := testdb.OpenDriver(t, database.DriverMySQL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	first, err := migrate.NewProvider(ctx, db, testdb.Migrations(t, database.DriverMySQL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.DownTo(ctx, 0); err != nil {
		t.Fatal(err)
	}
	second, err := migrate.NewProvider(ctx, db, testdb.Migrations(t, database.DriverMySQL))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, up := range []func(context.Context) error{
		func(ctx context.Context) error { _, err := first.Up(ctx); return err },
		func(ctx context.Context) error { _, err := second.Up(ctx); return err },
	} {
		wg.Go(func() {
			if err := up(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='cache_entries'").Scan(&count); err != nil || count != 1 {
		t.Fatal("cache migration was not applied", count, err)
	}
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	const name = "CONCAT('labena:',LEFT(SHA2(DATABASE(),256),48))"
	var acquired int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK("+name+",1)").Scan(&acquired); err != nil || acquired != 1 {
		t.Fatal(acquired, err)
	}
	defer conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK("+name+")")
	blocked, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	// Up can return early when nothing is pending; Down must acquire the lock.
	if _, err := first.Down(blocked); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
