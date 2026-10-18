// Package repository owns Identity persistence using PostgreSQL and the application pool.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/pkg/pagination"
)

// UserRepository borrows the caller context, including its transaction scope.
type UserRepository interface {
	// Create persists the account within the caller’s transaction when present.
	Create(ctx context.Context, user *entities.User) error
	// Edit locks the account, applies an in-memory change, validates and persists it
	// atomically. The callback must not perform external I/O or retain the user.
	// It joins a caller-owned transaction when present.
	Edit(ctx context.Context, userID uuid.UUID, change func(*entities.User) error) (*entities.User, error)
	Get(ctx context.Context, userID uuid.UUID) (*entities.User, error)
	List(ctx context.Context, page pagination.Pagination) ([]*entities.User, int, error)
}

// SessionRepository owns login and rotation transactions. Reuse revocation commits
// even when rotation denies access, so those operations reject outer transactions.
// Explicit RevokeSession/RevokeSessions can join a caller transaction for atomic cleanup.
type SessionRepository interface {
	// CompleteSignIn returns the authenticated account and whether it was created,
	// only after account, proof consumption and session commit together.
	CompleteSignIn(ctx context.Context, proof ChallengeProof, session *entities.Session, refresh *entities.SessionRefreshToken, allowConcurrentSessions bool, name string) (*entities.User, bool, error)
	RotateRefresh(ctx context.Context, digest []byte, next *entities.SessionRefreshToken, policy domain.SessionPolicy) (*entities.Session, error)
	AuthenticateSession(ctx context.Context, userID, sessionID uuid.UUID) error
	ListSessions(ctx context.Context, userID uuid.UUID, page pagination.Pagination) ([]*entities.Session, int, error)
	RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error
	RevokeSessions(ctx context.Context, userID uuid.UUID) error
	PruneSessions(ctx context.Context, limit int) (int64, error)
}

// ChallengeProof contains server-derived proof data, never the plaintext code.
// Purpose and policy are selected by the use case, not supplied by the client.
// Digest slices are borrowed only for the duration of the repository call.
type ChallengeProof struct {
	ID                  uuid.UUID
	Digest, Destination []byte
	Policy              domain.VerificationCodePolicy
	Purpose             enums.VerificationPurpose
}

// VerificationRepository owns each transaction: per-code attempts must commit even on an
// invalid code. Do not call its write operations inside another transaction.
type VerificationRepository interface {
	IssueEmailChallenge(ctx context.Context, challenge *entities.VerificationChallenge, policy domain.VerificationCodePolicy) error
	GetChallenge(ctx context.Context, challengeID uuid.UUID) (*entities.VerificationChallenge, error)
	PruneVerification(ctx context.Context, limit int) (int64, error)
}

// SocialRepository owns atomic account/identity/session writes.
type SocialRepository interface {
	// CompleteSocialSignIn returns the authenticated account and whether it was
	// created, after committing the account, provider identity and session.
	CompleteSocialSignIn(ctx context.Context, provider enums.AuthProvider, identity domain.SocialIdentity, session *entities.Session, refresh *entities.SessionRefreshToken, allowConcurrentSessions bool) (*entities.User, bool, error)
}
