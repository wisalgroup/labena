package identity_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	identitymigrations "github.com/wisalgroup/labena/internal/modules/identity/migrations"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
)

func TestAuthenticationStoragePostgres(t *testing.T) {
	db := testdb.OpenDriver(t, database.DriverPostgres)
	var registry migrate.Registry
	if err := registry.Register("test/cache", testdb.Migrations(t, database.DriverPostgres)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("identity", identitymigrations.Files); err != nil {
		t.Fatal(err)
	}
	history, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := migrate.NewProvider(t.Context(), db, history)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	if applied, err := provider.Up(t.Context()); err != nil || len(applied) != 0 {
		t.Fatal("migration not repeatable", applied, err)
	}
	email, err := valueobjects.NewEmail("account@example.com")
	if err != nil {
		t.Fatal(err)
	}
	user, err := entities.NewUser("Account", email)
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.NewUserRepository(db).Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	invalidUser := *user
	invalidUser.ID = uuid.New()
	invalidUser.Name = " "
	if err := repository.NewUserRepository(db).Create(t.Context(), &invalidUser); !errors.Is(err, entities.ErrInvalidUserName) {
		t.Fatal("application validation did not replace removed CHECK", err)
	}
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	identity := &entities.AuthIdentity{ID: uuid.New(), UserID: user.ID, Provider: enums.ProviderGoogle, ProviderSubject: "provider-subject", CreatedAt: now}
	challenge := &entities.VerificationChallenge{ID: uuid.New(), Purpose: enums.PurposeRegistration, Channel: enums.ChannelEmail, Destination: "new@example.com", CodeDigest: bytes.Repeat([]byte{1}, 32), MaxAttempts: 5, CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
	expires := now.Add(time.Hour)
	session := &entities.Session{ID: uuid.New(), UserID: user.ID, Platform: enums.PlatformWeb, CreatedAt: now, LastUsedAt: now, ExpiresAt: &expires}
	token := &entities.SessionRefreshToken{ID: uuid.New(), SessionID: session.ID, TokenHash: bytes.Repeat([]byte{2}, 32), CreatedAt: now, ExpiresAt: &expires}
	for _, record := range []interface{ Validate() error }{identity, challenge, session, token} {
		if err = record.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, err = db.NewInsert().Model(record).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	readChallenge := entities.VerificationChallenge{ID: challenge.ID}
	if err = db.NewSelect().Model(&readChallenge).WherePK().Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	if readChallenge.UserID != nil || readChallenge.ConsumedAt != nil || !bytes.Equal(readChallenge.CodeDigest, challenge.CodeDigest) {
		t.Fatal("challenge round trip failed")
	}
	readSession := entities.Session{ID: session.ID}
	if err = db.NewSelect().Model(&readSession).WherePK().Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	if readSession.DeviceName != nil || readSession.RevokedAt != nil || !readSession.ExpiresAt.Equal(*session.ExpiresAt) {
		t.Fatal("session round trip failed")
	}
	consumed := now.Add(time.Second)
	token.ConsumedAt = &consumed
	if _, err = db.NewUpdate().Model(token).Column("consumed_at").WherePK().Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	readToken := entities.SessionRefreshToken{ID: token.ID}
	if err = db.NewSelect().Model(&readToken).WherePK().Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	if readToken.ConsumedAt == nil || !readToken.ConsumedAt.Equal(consumed) || !bytes.Equal(readToken.TokenHash, token.TokenHash) {
		t.Fatal("refresh history round trip failed")
	}
	assertCode := func(err error, code string) {
		t.Helper()
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != code {
			t.Fatalf("expected SQLSTATE %s, got %v", code, err)
		}
	}
	duplicate := *identity
	duplicate.ID = uuid.New()
	_, err = db.NewInsert().Model(&duplicate).Exec(t.Context())
	assertCode(err, "23505")
	anotherToken := *token
	anotherToken.ID = uuid.New()
	_, err = db.NewInsert().Model(&anotherToken).Exec(t.Context())
	assertCode(err, "23505")
	orphan := *session
	orphan.ID = uuid.New()
	orphan.UserID = uuid.New()
	_, err = db.NewInsert().Model(&orphan).Exec(t.Context())
	assertCode(err, "23503")
	// Child history prevents accidental deletion of an account or session.
	_, err = db.NewDelete().Model(session).WherePK().Exec(t.Context())
	assertCode(err, "23503")
	_, err = db.NewDelete().Model(user).WherePK().Exec(t.Context())
	assertCode(err, "23503")
	var checks int
	err = db.NewRaw(`SELECT count(*) FROM pg_constraint WHERE connamespace = current_schema()::regnamespace AND contype = 'c' AND conrelid IN ('users'::regclass, 'auth_identities'::regclass, 'verification_challenges'::regclass, 'sessions'::regclass, 'session_refresh_tokens'::regclass)`).Scan(t.Context(), &checks)
	if err != nil || checks != 0 {
		t.Fatal("unexpected business CHECK constraints", checks, err)
	}
}
