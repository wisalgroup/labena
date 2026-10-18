package identity_test

import (
	"testing"

	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
)

func enumRoundTrip[T ~string](t *testing.T, db *bun.DB, values []T) {
	t.Helper()
	for _, value := range values {
		var got T
		if err := db.NewRaw("SELECT ?::text", value).Scan(t.Context(), &got); err != nil || got != value {
			t.Fatalf("Bun round trip %q: got %q, %v", value, got, err)
		}
		// Also exercise database/sql's native named-string conversion independently of Bun.
		if err := db.DB.QueryRowContext(t.Context(), "SELECT $1::text", value).Scan(&got); err != nil || got != value {
			t.Fatalf("SQL round trip %q: got %q, %v", value, got, err)
		}
	}
}

func TestIdentityEnumsPostgres(t *testing.T) {
	db := testdb.OpenDriver(t, database.DriverPostgres)
	enumRoundTrip(t, db, enums.UserStatusValues())
	enumRoundTrip(t, db, enums.AuthProviderValues())
	enumRoundTrip(t, db, enums.VerificationPurposeValues())
	enumRoundTrip(t, db, enums.VerificationChannelValues())
	enumRoundTrip(t, db, enums.SessionPlatformValues())
}
