package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/auth"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/pagination"
	"github.com/wisalgroup/labena/pkg/token"
)

func (s *authService) RequestSignInCode(ctx context.Context, email, ip string) (uuid.UUID, error) {
	if s.access == nil {
		return uuid.Nil, domain.ErrSessionConfig
	}
	return s.requestCode(ctx, email, ip, enums.PurposeSignIn)
}

// SignInWithCode verifies an OTP and creates a verified account if needed. New accounts
// and sessions commit together; registration events are best-effort after commit.
func (s *authService) SignInWithCode(ctx context.Context, input SignInInput) (*SignInResult, error) {
	if s.access == nil {
		return nil, domain.ErrSessionConfig
	}
	raw, session, refresh, err := s.newSession(input.Device)
	if err != nil {
		return nil, err
	}
	name, err := signupName(input.Name)
	if err != nil {
		return nil, err
	}
	proof, err := s.proof(ctx, input.Proof.ChallengeID, input.Proof.Code, input.Proof.ClientIP, enums.PurposeSignIn)
	if err != nil {
		return nil, err
	}
	user, created, err := s.sessions.CompleteSignIn(ctx, proof, session, refresh, s.sessionPolicy.AllowConcurrentSessions, name)
	if err != nil {
		return nil, err
	}
	if created {
		publishRegistration(ctx, s.eventPublisher, user)
	}
	tokens, err := s.tokens(session, refresh, raw)
	if err != nil {
		return nil, err
	}
	return &SignInResult{Tokens: tokens, User: user, Created: created}, nil
}
func (s *authService) Refresh(ctx context.Context, raw string) (*Tokens, error) {
	if s.access == nil {
		return nil, domain.ErrSessionConfig
	}
	hash, err := token.HashOpaque(raw)
	if err != nil {
		return nil, domain.ErrUnauthenticated
	}
	nextRaw, next, err := newRefresh()
	if err != nil {
		return nil, err
	}
	session, err := s.sessions.RotateRefresh(ctx, hash, next, s.sessionPolicy)
	if err != nil {
		return nil, err
	}
	return s.tokens(session, next, nextRaw)
}
func (s *authService) tokens(session *entities.Session, refresh *entities.SessionRefreshToken, raw string) (*Tokens, error) {
	now := time.Now().UTC()
	expires := now.Add(s.sessionPolicy.AccessTokenLifetime)
	if session.ExpiresAt != nil && expires.After(*session.ExpiresAt) {
		expires = *session.ExpiresAt
	}
	expires = expires.Truncate(time.Second)
	access, err := s.access.Issue(token.Principal{UserID: session.UserID, SessionID: session.ID}, expires)
	if err != nil {
		return nil, err
	}
	return &Tokens{AccessToken: access, RefreshToken: raw, TokenType: "Bearer", ExpiresIn: int64(expires.Sub(now) / time.Second), RefreshExpiresAt: refresh.ExpiresAt, SessionID: session.ID}, nil
}
func (s *authService) Authenticate(ctx context.Context, raw string) (auth.Principal, error) {
	if s.access == nil {
		return auth.Principal{}, domain.ErrUnauthenticated
	}
	principal, err := s.access.Parse(raw)
	if err != nil {
		return auth.Principal{}, domain.ErrUnauthenticated
	}
	if err := s.sessions.AuthenticateSession(ctx, principal.UserID, principal.SessionID); err != nil {
		return auth.Principal{}, err
	}
	return auth.Principal{UserID: principal.UserID, SessionID: principal.SessionID}, nil
}
func (s *authService) ListSessions(ctx context.Context, userID uuid.UUID, p pagination.Pagination) (*pagination.Page[*entities.Session], error) {
	if s.access == nil {
		return nil, domain.ErrSessionConfig
	}
	p = pagination.NewPagination(p.Page, p.PerPage)
	rows, total, err := s.sessions.ListSessions(ctx, userID, p)
	if err != nil {
		return nil, err
	}
	return pagination.NewPage(rows, p, total), nil
}
func (s *authService) RevokeSession(ctx context.Context, userID, id uuid.UUID) error {
	if s.access == nil {
		return domain.ErrSessionConfig
	}
	if s.notifier == nil {
		return s.sessions.RevokeSession(ctx, userID, id)
	}
	return s.transactions.Execute(ctx, func(ctx context.Context) error {
		if err := s.sessions.RevokeSession(ctx, userID, id); err != nil {
			return err
		}
		return s.notifier.DetachSession(ctx, notification.Recipient{Type: string(entities.UserEntityType), ID: userID.String()}, id.String())
	})
}
func (s *authService) RevokeSessions(ctx context.Context, userID uuid.UUID) error {
	if s.access == nil {
		return domain.ErrSessionConfig
	}
	if s.notifier == nil {
		return s.sessions.RevokeSessions(ctx, userID)
	}
	return s.transactions.Execute(ctx, func(ctx context.Context) error {
		if err := s.sessions.RevokeSessions(ctx, userID); err != nil {
			return err
		}
		return s.notifier.DetachSession(ctx, notification.Recipient{Type: string(entities.UserEntityType), ID: userID.String()}, "")
	})
}
func (s *authService) PruneSessions(ctx context.Context, limit int) (int64, error) {
	if s.access == nil {
		return 0, domain.ErrSessionConfig
	}
	return s.sessions.PruneSessions(ctx, limit)
}

func newRefresh() (string, *entities.SessionRefreshToken, error) {
	raw, hash, err := token.NewOpaque()
	if err != nil {
		return "", nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return "", nil, err
	}
	return raw, &entities.SessionRefreshToken{ID: id, TokenHash: hash}, nil
}
func (s *authService) newSession(input Device) (string, *entities.Session, *entities.SessionRefreshToken, error) {
	if !input.Platform.Valid() || utf8.RuneCountInString(input.Name) > 100 || strings.ContainsAny(input.Name, "\r\n") {
		return "", nil, nil, domain.ErrSessionInput
	}
	id, err := uuid.NewV7()
	if err != nil {
		return "", nil, nil, err
	}
	raw, refresh, err := newRefresh()
	if err != nil {
		return "", nil, nil, err
	}
	now := time.Now().UTC()
	session := &entities.Session{ID: id, Platform: input.Platform, CreatedAt: now, LastUsedAt: now}
	if s.sessionPolicy.AbsoluteSessionLifetime > 0 {
		expires := now.Add(s.sessionPolicy.AbsoluteSessionLifetime)
		session.ExpiresAt = &expires
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		session.DeviceName = &name
	}
	refresh.SessionID = id
	refresh.CreatedAt = now
	refresh.ExpiresAt = session.ExpiresAt
	if s.sessionPolicy.RefreshTokenLifetime > 0 {
		expires := now.Add(s.sessionPolicy.RefreshTokenLifetime)
		if refresh.ExpiresAt == nil || expires.Before(*refresh.ExpiresAt) {
			refresh.ExpiresAt = &expires
		}
	}
	return raw, session, refresh, nil
}

// SubscribePush serializes registration with revocation using the same account
// lock. A request authenticated before logout cannot reattach the revoked session.
func (s *authService) SubscribePush(ctx context.Context, principal auth.Principal, installationID, registrationToken string) (*notification.Subscription, error) {
	if s.access == nil {
		return nil, domain.ErrSessionConfig
	}
	if s.notifier == nil {
		return nil, notification.ErrUnavailable
	}
	var subscription *notification.Subscription
	err := s.transactions.Execute(ctx, func(ctx context.Context) error {
		if err := s.sessions.AuthenticateSession(ctx, principal.UserID, principal.SessionID); err != nil {
			return err
		}
		var err error
		subscription, err = s.notifier.Subscribe(ctx, notification.Recipient{Type: string(entities.UserEntityType), ID: principal.UserID.String()}, installationID, principal.SessionID.String(), registrationToken)
		return err
	})
	return subscription, err
}
