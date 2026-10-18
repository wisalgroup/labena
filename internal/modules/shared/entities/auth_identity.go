package entities

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/shared/enums"
)

// AuthIdentity links a provider subject to an account. Identity owns writes.
// Provider subjects are opaque and must never be normalized as email addresses.
type AuthIdentity struct {
	bun.BaseModel       `bun:"table:auth_identities,alias:ai"`
	ID                  uuid.UUID          `bun:"id,pk,type:uuid"`
	UserID              uuid.UUID          `bun:"user_id,type:uuid,notnull"`
	Provider            enums.AuthProvider `bun:"provider,notnull"`
	ProviderSubject     string             `bun:"provider_subject,notnull" json:"-"`
	CreatedAt           time.Time          `bun:"created_at,notnull"`
	LastAuthenticatedAt *time.Time         `bun:"last_authenticated_at"`
}

// Validate checks stored values; it does not verify provider credentials or authorize linking.
func (i AuthIdentity) Validate() error {
	if i.ID == uuid.Nil || i.UserID == uuid.Nil {
		return ErrInvalidAuthIdentityIDs
	}
	if !i.Provider.Valid() {
		return ErrUnsupportedAuthProvider
	}
	if strings.TrimSpace(i.ProviderSubject) == "" {
		return ErrMissingProviderSubject
	}
	if i.CreatedAt.IsZero() || (i.LastAuthenticatedAt != nil && i.LastAuthenticatedAt.Before(i.CreatedAt)) {
		return ErrInvalidAuthIdentityTimestamps
	}
	return nil
}
