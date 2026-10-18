package app_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/app"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/token"
)

// Exercise app's actual construction and security wiring through public entry
// points. Seed a session so the test needs no external mail or identity provider.
func TestApplicationAuthenticationAndSessionPush(t *testing.T) {
	queueURL, limiterURL := os.Getenv("QUEUE_TEST_REDIS"), os.Getenv("RATE_LIMIT_TEST_REDIS")
	if queueURL == "" || limiterURL == "" {
		t.Skip("QUEUE_TEST_REDIS and RATE_LIMIT_TEST_REDIS not set")
	}
	cfg, db := databaseConfig(t)
	if err := app.Execute(t.Context(), cfg, app.Processes{}, []string{"migrate", "up"}); err != nil {
		t.Fatal(err)
	}
	cfg.Queue.RedisURL = queueURL
	cfg.RateLimit.RedisURL = limiterURL
	cfg.Auth.AccessTokenKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	cfg.Auth.VerificationKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("v", 32)))
	cfg.Push.Enabled = true
	cfg.Push.ProjectID = "app-auth-test"

	email, err := valueobjects.NewEmail("app-auth@example.com")
	if err != nil {
		t.Fatal(err)
	}
	user, err := entities.NewUser("App auth", email)
	if err != nil {
		t.Fatal(err)
	}
	if err := user.VerifyEmail(email, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewUserRepository(db).Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	session := &entities.Session{ID: uuid.New(), UserID: user.ID, Platform: enums.PlatformWeb, CreatedAt: time.Now().UTC(), LastUsedAt: time.Now().UTC()}
	if _, err := db.NewInsert().Model(session).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	signer, err := token.NewAccess([]byte(strings.Repeat("a", 32)), cfg.Auth.TokenIssuer, cfg.Auth.TokenAudience)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := signer.Issue(token.Principal{UserID: user.ID, SessionID: session.ID}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + cfg.HTTP.Address + "/api/v1"
	request := func(method, path, body string) *http.Request {
		r, err := http.NewRequestWithContext(t.Context(), method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	installation := uuid.NewString()
	payload := fmt.Sprintf(`{"installation_id":%q,"registration":"ExpoPushToken[app-auth]"}`, installation)

	stop := serve(t, cfg, app.Processes{HTTP: true})
	for _, r := range []*http.Request{request("GET", "/users/me", ""), request("POST", "/notifications/subscriptions", payload)} {
		res, body := requestBody(t, r)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("disabled auth accepted a valid session: %d %s", res.StatusCode, body)
		}
	}
	stop()
	cfg.Auth.Enabled = true
	stop = serve(t, cfg, app.Processes{HTTP: true})
	defer stop()
	res, body := requestBody(t, request("GET", "/users/me", ""))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("configured authenticator: %d %s", res.StatusCode, body)
	}
	res, body = requestBody(t, request("POST", "/notifications/subscriptions", payload))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("session-aware push: %d %s", res.StatusCode, body)
	}
	var envelope struct{ Data struct{ ID uuid.UUID } }
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data.ID == uuid.Nil {
		t.Fatal("invalid subscription response", string(body), err)
	}
	stored := &notification.Subscription{ID: envelope.Data.ID}
	if err := db.NewSelect().Model(stored).WherePK().Scan(t.Context()); err != nil || stored.SessionID != session.ID.String() {
		t.Fatal("subscription did not retain session ownership", stored, err)
	}

	// Bearer checks are reads, but push registration must reauthorize under the
	// account lock. Commit revocation while that registration waits for the lock.
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var locked uuid.UUID
	if err := tx.NewSelect().Table("users").Column("id").Where("id = ?", user.ID).For("UPDATE").Scan(t.Context(), &locked); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.NewUpdate().Model(session).Set("revoked_at = ?", time.Now().UTC()).WherePK().Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	newInstallation := uuid.NewString()
	racingRequest := request("POST", "/notifications/subscriptions", fmt.Sprintf(`{"installation_id":%q,"registration":"ExpoPushToken[racing]"}`, newInstallation))
	type result struct {
		code int
		err  error
	}
	finished := make(chan result, 1)
	go func() {
		res, err := (&http.Client{Timeout: 5 * time.Second}).Do(racingRequest)
		if err != nil {
			finished <- result{err: err}
			return
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		finished <- result{code: res.StatusCode}
	}()
	dsn, _ := url.Parse(cfg.Database.DSN)
	waitFor(t, func() bool {
		select {
		case early := <-finished:
			t.Fatalf("push registration bypassed the session lock: %+v", early)
		default:
		}
		var count int
		err := db.NewRaw("SELECT count(*) FROM pg_stat_activity WHERE application_name = ? AND wait_event_type = 'Lock'", dsn.Query().Get("application_name")).Scan(t.Context(), &count)
		return err == nil && count > 0
	})
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-finished:
		if got.err != nil || got.code != http.StatusUnauthorized {
			t.Fatalf("revoked session registered push: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("push registration did not finish after revocation")
	}
	count, err := db.NewSelect().Model((*notification.Subscription)(nil)).Where("installation_id = ?", newInstallation).Count(t.Context())
	if err != nil || count != 0 {
		t.Fatal("revoked session created a subscription", count, err)
	}
	stop()
	assertAppConnectionsClosed(t, db, cfg)
}
