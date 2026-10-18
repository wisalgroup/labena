package identity_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/pagination"
	"github.com/wisalgroup/labena/pkg/token"
)

func TestSessionsPostgres(t *testing.T) {
	db := verificationDB(t)
	repo := newRepositories(db)
	sender := &codeDelivery{codes: map[string]string{}}
	key := bytes.Repeat([]byte{0x42}, 32)
	access, err := token.NewAccess(bytes.Repeat([]byte{0x11}, 32), "test", "api")
	if err != nil {
		t.Fatal(err)
	}
	policy := domain.SessionPolicy{AccessTokenLifetime: 15 * time.Minute, RefreshTokenLifetime: 7 * 24 * time.Hour, AbsoluteSessionLifetime: 30 * 24 * time.Hour, AllowConcurrentSessions: true}
	options := authenticationOptions(t, repo, key, sender)
	options.Access = access
	options.SessionPolicy = policy
	svc, err := service.NewAuthService(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("sign-in resends share send admission", func(t *testing.T) {
		const email = "shared-admission@example.com"
		const ip = "192.0.2.240"
		for range domain.DefaultAuthRateLimitPolicy().CodeRequestsPerDestination {
			if _, err := db.NewRaw("UPDATE verification_challenges SET created_at = ? WHERE destination = ?", time.Now().Add(-2*time.Minute), email).Exec(t.Context()); err != nil {
				t.Fatal(err)
			}
			_, err := svc.RequestSignInCode(t.Context(), email, ip)
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err := svc.RequestSignInCode(t.Context(), email, ip); !errors.Is(err, domain.ErrVerificationLimited) {
			t.Fatal("verification escaped shared budget", err)
		}
		if _, err := svc.RequestSignInCode(t.Context(), email, ip); !errors.Is(err, domain.ErrVerificationLimited) {
			t.Fatal("sign-in escaped shared budget", err)
		}
	})
	create := func(email string, verified bool) *entities.User {
		address, _ := valueobjects.NewEmail(email)
		user, err := entities.NewUser("Sessions", address)
		if err != nil {
			t.Fatal(err)
		}
		if verified {
			if err := user.VerifyEmail(address, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.users.Create(t.Context(), user); err != nil {
			t.Fatal(err)
		}
		return user
	}
	requestCount := 0
	request := func(email string) service.SignInInput {
		// Advance the persisted cooldown, rather than sleeping in a test.
		if _, err := db.NewRaw("UPDATE verification_challenges SET created_at = LEAST(created_at, ?)", time.Now().Add(-2*time.Minute)).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		requestCount++
		clientIP := fmt.Sprintf("192.0.2.%d", requestCount)
		id, err := svc.RequestSignInCode(t.Context(), email, clientIP)
		if err != nil {
			t.Fatal(err)
		}
		return service.SignInInput{Proof: service.ProofInput{ChallengeID: id, Code: sender.code(email), ClientIP: clientIP}, Device: service.Device{Platform: enums.PlatformWeb, Name: "Browser"}}
	}
	login := func(user *entities.User) *service.SignInResult {
		tokens, err := svc.SignInWithCode(t.Context(), request(user.Email.String()))
		if err != nil {
			t.Fatal(err)
		}
		return tokens
	}
	t.Run("verified account login rotation and strict reuse", func(t *testing.T) {
		user := create("rotation@example.com", true)
		tokens := login(user)
		principal, err := svc.Authenticate(t.Context(), tokens.AccessToken)
		if err != nil || principal.UserID != user.ID || principal.SessionID != tokens.SessionID {
			t.Fatal(principal, err)
		}
		next, err := svc.Refresh(t.Context(), tokens.RefreshToken)
		if err != nil || next.RefreshToken == tokens.RefreshToken || next.SessionID != tokens.SessionID {
			t.Fatal("rotation failed", err)
		}
		if _, err := svc.Authenticate(t.Context(), next.AccessToken); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Refresh(t.Context(), tokens.RefreshToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("reuse accepted", err)
		}
		if _, err := svc.Authenticate(t.Context(), next.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("replay did not revoke family", err)
		}
		if _, err := svc.Refresh(t.Context(), next.RefreshToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("revoked refresh accepted", err)
		}
		history, err := db.NewSelect().Model((*entities.SessionRefreshToken)(nil)).Where("session_id = ?", tokens.SessionID).Count(t.Context())
		if err != nil || history != 2 {
			t.Fatal("history missing", history, err)
		}
	})
	t.Run("failed successor insert rolls back consumption", func(t *testing.T) {
		user := create("rollback-refresh@example.com", true)
		tokens := login(user)
		raw, err := base64.RawURLEncoding.DecodeString(tokens.RefreshToken)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(raw)
		old := new(entities.SessionRefreshToken)
		if err := db.NewSelect().Model(old).Where("token_hash = ?", hash[:]).Scan(t.Context()); err != nil {
			t.Fatal(err)
		}
		// A duplicate primary key fails after consumption was written in the transaction.
		next := &entities.SessionRefreshToken{ID: old.ID, TokenHash: bytes.Repeat([]byte{0x77}, 32)}
		if _, err := repo.sessions.RotateRefresh(t.Context(), hash[:], next, policy); err == nil {
			t.Fatal("duplicate successor accepted")
		}
		if err := db.NewSelect().Model(old).WherePK().Scan(t.Context()); err != nil {
			t.Fatal(err)
		}
		if old.ConsumedAt != nil {
			t.Fatal("failed rotation consumed the credential")
		}
		if _, err := svc.Refresh(t.Context(), tokens.RefreshToken); err != nil {
			t.Fatal("credential was not usable after rollback", err)
		}
	})
	t.Run("concurrent refresh returns one successor then revokes on replay", func(t *testing.T) {
		user := create("concurrent-refresh@example.com", true)
		tokens := login(user)
		results := make(chan *service.Tokens, 2)
		failures := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() { next, err := svc.Refresh(t.Context(), tokens.RefreshToken); results <- next; failures <- err })
		}
		wg.Wait()
		close(results)
		close(failures)
		successes := 0
		var successor *service.Tokens
		for next := range results {
			if next != nil {
				successes++
				successor = next
			}
		}
		if successes != 1 {
			t.Fatal("expected one rotation", successes)
		}
		for err := range failures {
			if err != nil && !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatal(err)
			}
		}
		if _, err := svc.Authenticate(t.Context(), successor.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("concurrent replay not revoked", err)
		}
	})
	t.Run("new account is created only after valid proof", func(t *testing.T) {
		input := request("new-account@example.com")
		input.Name = "New Member"
		count, err := db.NewSelect().Model((*entities.User)(nil)).Where("email = ?", "new-account@example.com").Count(t.Context())
		if err != nil || count != 0 {
			t.Fatal("account created before proof", count, err)
		}
		wrong := input
		wrong.Proof.Code = "invalid"
		if _, err := svc.SignInWithCode(t.Context(), wrong); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal(err)
		}
		count, _ = db.NewSelect().Model((*entities.User)(nil)).Where("email = ?", "new-account@example.com").Count(t.Context())
		if count != 0 {
			t.Fatal("invalid proof created account")
		}
		tokens, err := svc.SignInWithCode(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		principal, err := svc.Authenticate(t.Context(), tokens.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		user, err := repo.users.Get(t.Context(), principal.UserID)
		if err != nil || user.EmailVerifiedAt == nil || user.Name != "New Member" {
			t.Fatal(user, err)
		}
		if err := user.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SignInWithCode(t.Context(), input); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal("proof reused", err)
		}
		again := request(user.Email.String())
		again.Name = "Do not replace name"
		next, err := svc.SignInWithCode(t.Context(), again)
		if err != nil {
			t.Fatal(err)
		}
		if !tokens.Created || next.Created {
			t.Fatal("wrong account creation result")
		}
		nextPrincipal, err := svc.Authenticate(t.Context(), next.AccessToken)
		if err != nil || nextPrincipal.UserID != user.ID {
			t.Fatal("duplicate account or event", err)
		}
		user, _ = repo.users.Get(t.Context(), user.ID)
		if user.Name != "New Member" {
			t.Fatal("existing profile overwritten")
		}
	})
	t.Run("concurrent signup consumes one proof and creates one account", func(t *testing.T) {
		input := request("concurrent-signup@example.com")
		start := make(chan struct{})
		results := make(chan error, 2)
		for range 2 {
			go func() { <-start; _, err := svc.SignInWithCode(t.Context(), input); results <- err }()
		}
		close(start)
		successes := 0
		for range 2 {
			err := <-results
			if err == nil {
				successes++
			} else if !errors.Is(err, domain.ErrVerificationInvalid) {
				t.Fatal(err)
			}
		}
		count, err := db.NewSelect().Model((*entities.User)(nil)).Where("email = ?", "concurrent-signup@example.com").Count(t.Context())
		if err != nil || successes != 1 || count != 1 {
			t.Fatal(successes, count, err)
		}
	})
	t.Run("expired signup proof cannot create account", func(t *testing.T) {
		input := request("expired-signup@example.com")
		if _, err := db.NewUpdate().Model((*entities.VerificationChallenge)(nil)).Set("expires_at = ?", time.Now().Add(-time.Second)).Where("id = ?", input.Proof.ChallengeID).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SignInWithCode(t.Context(), input); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal(err)
		}
		count, err := db.NewSelect().Model((*entities.User)(nil)).Where("email = ?", "expired-signup@example.com").Count(t.Context())
		if err != nil || count != 0 {
			t.Fatal(count, err)
		}
	})
	t.Run("reserved disabled email cannot create replacement account", func(t *testing.T) {
		user := create("reserved-email@example.com", true)
		if _, err := repo.users.Edit(t.Context(), user.ID, func(u *entities.User) error { return u.ChangeStatus(enums.UserPendingDeletion, time.Now().UTC()) }); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.users.Edit(t.Context(), user.ID, func(u *entities.User) error { return u.ChangeStatus(enums.UserDeleted, time.Now().UTC()) }); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SignInWithCode(t.Context(), request(user.Email.String())); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal(err)
		}
		count, err := db.NewSelect().Model((*entities.User)(nil)).Where("email = ?", user.Email.String()).Count(t.Context())
		if err != nil || count != 1 {
			t.Fatal(count, err)
		}
	})
	t.Run("bound proof cannot create account after email change", func(t *testing.T) {
		user := create("old-contact@example.com", true)
		input := request(user.Email.String())
		email, _ := valueobjects.NewEmail("new-contact@example.com")
		if _, err := repo.users.Edit(t.Context(), user.ID, func(u *entities.User) error { return u.ChangeEmail(email, time.Now().UTC()) }); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SignInWithCode(t.Context(), input); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal(err)
		}
		count, err := db.NewSelect().Model((*entities.User)(nil)).Where("email = ?", "old-contact@example.com").Count(t.Context())
		if err != nil || count != 0 {
			t.Fatal(count, err)
		}
	})
	t.Run("signup publication follows commit and failure preserves credentials", func(t *testing.T) {
		const email = "publication@example.com"
		published := &registrationRecorder{check: func(ctx context.Context) error {
			var user entities.User
			if err := db.NewSelect().Model(&user).Where("email = ?", email).Scan(ctx); err != nil || user.EmailVerifiedAt == nil {
				t.Fatal("published before account committed", err)
			}
			count, err := db.NewSelect().Model((*entities.Session)(nil)).Where("user_id = ?", user.ID).Count(ctx)
			if err != nil || count == 0 {
				t.Fatal("published before session committed", err)
			}
			return errors.New("local bus unavailable")
		}}
		input := options
		input.EventPublisher = published
		auth, err := service.NewAuthService(input)
		if err != nil {
			t.Fatal(err)
		}
		for _, created := range []bool{true, false} {
			result, err := auth.SignInWithCode(t.Context(), request(email))
			if err != nil || result == nil {
				t.Fatal("publication failure changed committed sign-in", err)
			}
			if result.Created != created || published.calls != 1 {
				t.Fatal("registration reaction repeated or wrong creation flag")
			}
			if _, err := auth.Authenticate(t.Context(), result.AccessToken); err != nil {
				t.Fatal("missing committed session", err)
			}
		}
	})
	t.Run("nameless signup and profile update", func(t *testing.T) {
		result, err := svc.SignInWithCode(t.Context(), request("nameless@example.com"))
		if err != nil {
			t.Fatal(err)
		}
		if !result.Created {
			t.Fatal("missing creation result")
		}
		principal, err := svc.Authenticate(t.Context(), result.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		user, err := repo.users.Get(t.Context(), principal.UserID)
		if err != nil || user.Name != "" || user.ProfileComplete() || result.User.ID != user.ID || result.User.ProfileComplete() {
			t.Fatal(user, err)
		}
		users, err := service.NewUserService(repo.users)
		if err != nil {
			t.Fatal(err)
		}
		name := "  Updated Name  "
		if _, err := users.UpdateProfile(t.Context(), user.ID, service.UpdateProfileInput{Name: &name}); err != nil {
			t.Fatal(err)
		}
		user, err = repo.users.Get(t.Context(), user.ID)
		if err != nil || user.Name != "Updated Name" || user.EmailVerifiedAt == nil {
			t.Fatal(user, err)
		}
		if _, err := svc.Authenticate(t.Context(), result.AccessToken); err != nil {
			t.Fatal("profile edit revoked session", err)
		}
	})
	t.Run("unverified active account is verified and signed in", func(t *testing.T) {
		user := create("previously-unverified@example.com", false)
		tokens := login(user)
		principal, err := svc.Authenticate(t.Context(), tokens.AccessToken)
		if err != nil || principal.UserID != user.ID {
			t.Fatal(principal, err)
		}
		user, err = repo.users.Get(t.Context(), user.ID)
		if err != nil || user.EmailVerifiedAt == nil {
			t.Fatal(user, err)
		}
	})
	t.Run("registration racing a pending proof uses the same account", func(t *testing.T) {
		input := request("registration-race@example.com")
		user := create("registration-race@example.com", false)
		tokens, err := svc.SignInWithCode(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		principal, err := svc.Authenticate(t.Context(), tokens.AccessToken)
		if err != nil || principal.UserID != user.ID {
			t.Fatal(principal, err)
		}
	})
	t.Run("failed session creation rolls back signup and proof", func(t *testing.T) {
		input := request("signup-rollback@example.com")
		c, err := repo.verification.GetChallenge(t.Context(), input.Proof.ChallengeID)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		expires := now.Add(time.Hour)
		session := &entities.Session{ID: uuid.New(), Platform: enums.PlatformWeb, CreatedAt: now, LastUsedAt: now, ExpiresAt: &expires}
		// Validation fails after account creation in the transaction.
		refresh := &entities.SessionRefreshToken{}
		if _, _, err := repo.sessions.CompleteSignIn(t.Context(), repository.ChallengeProof{ID: c.ID, Digest: c.CodeDigest, Destination: bytes.Repeat([]byte{8}, 32), Policy: domain.DefaultVerificationCodePolicy()}, session, refresh, true, "User"); err == nil {
			t.Fatal("invalid refresh accepted")
		}
		count, err := db.NewSelect().Model((*entities.User)(nil)).Where("email = ?", c.Destination).Count(t.Context())
		if err != nil || count != 0 {
			t.Fatal("failed session left an account", count, err)
		}
		c, err = repo.verification.GetChallenge(t.Context(), input.Proof.ChallengeID)
		if err != nil || c.ConsumedAt != nil || c.UserID != nil {
			t.Fatal("proof changed on rollback", err)
		}
		if _, err := svc.SignInWithCode(t.Context(), input); err != nil {
			t.Fatal("proof unusable after rollback", err)
		}
	})
	t.Run("wrong purpose and inactive accounts denied", func(t *testing.T) {
		unverified := create("unverified@example.com", false)
		if _, err := db.NewRaw("UPDATE verification_challenges SET created_at = LEAST(created_at, ?)", time.Now().Add(-2*time.Minute)).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		id, err := svc.RequestSignInCode(t.Context(), unverified.Email.String(), "192.0.2.2")
		if err != nil {
			t.Fatal(err)
		}
		// Simulate a legacy proof from the retired standalone verification flow.
		if _, err := db.NewRaw("UPDATE verification_challenges SET purpose = ? WHERE id = ?", enums.PurposeVerifyEmail, id).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		input := service.SignInInput{Proof: service.ProofInput{ChallengeID: id, Code: sender.code(unverified.Email.String()), ClientIP: "192.0.2.2"}, Device: service.Device{Platform: enums.PlatformIOS}}
		if _, err := svc.SignInWithCode(t.Context(), input); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal("verification code used for login", err)
		}
		user := create("inactive@example.com", true)
		input = request(user.Email.String())
		if _, err := repo.users.Edit(t.Context(), user.ID, func(u *entities.User) error { return u.ChangeStatus(enums.UserSuspended, time.Now().UTC()) }); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SignInWithCode(t.Context(), input); !errors.Is(err, domain.ErrVerificationInvalid) {
			t.Fatal("suspended account logged in", err)
		}
	})
	t.Run("multiple sessions and recipient scoped management", func(t *testing.T) {
		user := create("multiple@example.com", true)
		first := login(user)
		second := login(user)
		page, err := svc.ListSessions(t.Context(), user.ID, pagination.NewPagination(1, 1))
		if err != nil || page.Total != 2 || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
		other := create("other@example.com", true)
		if err := svc.RevokeSession(t.Context(), other.ID, first.SessionID); !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatal("revoked other user's session", err)
		}
		if err := svc.RevokeSession(t.Context(), user.ID, first.SessionID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(t.Context(), first.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(t.Context(), second.AccessToken); err != nil {
			t.Fatal(err)
		}
		if err := svc.RevokeSessions(t.Context(), user.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(t.Context(), second.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal(err)
		}
	})
	t.Run("single session concurrent login serializes by account", func(t *testing.T) {
		user := create("single@example.com", true)
		first := request(user.Email.String())
		second := request(user.Email.String())
		// Seed two independently valid proofs to exercise account-level serialization
		// independently of the current one-outstanding-code request policy.
		if _, err := db.NewRaw("UPDATE verification_challenges SET invalidated_at = NULL WHERE id = ?", first.Proof.ChallengeID).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		singlePolicy := policy
		singlePolicy.AllowConcurrentSessions = false
		singleOptions := options
		singleOptions.SessionPolicy = singlePolicy
		single, err := service.NewAuthService(singleOptions)
		if err != nil {
			t.Fatal(err)
		}
		results := make(chan *service.SignInResult, 2)
		var wg sync.WaitGroup
		for _, input := range []service.SignInInput{first, second} {
			wg.Go(func() {
				tokens, err := single.SignInWithCode(t.Context(), input)
				if err != nil {
					t.Error(err)
				}
				results <- tokens
			})
		}
		wg.Wait()
		close(results)
		active := 0
		for tokens := range results {
			if tokens == nil {
				continue
			}
			if _, err := single.Authenticate(t.Context(), tokens.AccessToken); err == nil {
				active++
			} else if !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatal(err)
			}
		}
		if active != 1 {
			t.Fatal("single-session policy violated", active)
		}
	})
	t.Run("contact or status change revokes and cannot resurrect sessions", func(t *testing.T) {
		user := create("suspend@example.com", true)
		tokens := login(user)
		for _, status := range []enums.UserStatus{enums.UserSuspended, enums.UserActive} {
			if _, err := repo.users.Edit(t.Context(), user.ID, func(u *entities.User) error { return u.ChangeStatus(status, time.Now().UTC()) }); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := svc.Authenticate(t.Context(), tokens.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("resurrected session", err)
		}
		tokens = login(user)
		if _, err := repo.users.Edit(t.Context(), user.ID, func(u *entities.User) error {
			email, _ := valueobjects.NewEmail("new-address@example.com")
			return u.ChangeEmail(email, time.Now().UTC())
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(t.Context(), tokens.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("contact change left access valid", err)
		}
	})
	t.Run("expiry cleanup and outer transaction refusal", func(t *testing.T) {
		user := create("expiry@example.com", true)
		tokens := login(user)
		tx, _ := database.NewTxManager(db, nil)
		err := tx.Execute(t.Context(), func(ctx context.Context) error { _, err := svc.Refresh(ctx, tokens.RefreshToken); return err })
		if !errors.Is(err, database.ErrTransactionActive) {
			t.Fatal(err)
		}
		if _, err := db.NewRaw("UPDATE session_refresh_tokens SET expires_at = ? WHERE session_id = ?", time.Now().Add(-time.Minute), tokens.SessionID).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Refresh(t.Context(), tokens.RefreshToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("expired refresh accepted", err)
		}
		if _, err := db.NewRaw("UPDATE sessions SET expires_at = ? WHERE id = ?", time.Now().Add(-time.Minute), tokens.SessionID).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(t.Context(), tokens.AccessToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("expired session accepted", err)
		}
		count, err := svc.PruneSessions(t.Context(), 1)
		if err != nil || count != 2 {
			t.Fatal("bounded cleanup", count, err)
		}
	})
}
