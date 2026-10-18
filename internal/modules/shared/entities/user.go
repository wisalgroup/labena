package entities

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/modules/shared/domain"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
)

// User is owned by Identity.
// It is both the domain entity and Bun model for users; persistence and domain
// shape intentionally evolve together. HTTP uses an explicit response DTO.
type User struct {
	bun.BaseModel `bun:"table:users,alias:u"`
	domain.Entity `bun:",embed"`

	Email           valueobjects.Email `json:"email" bun:"email,type:text,notnull"`
	EmailVerifiedAt *time.Time         `json:"email_verified_at" bun:"email_verified_at"`
	Phone           *string            `json:"phone" bun:"phone"`
	PhoneVerifiedAt *time.Time         `json:"phone_verified_at" bun:"phone_verified_at"`
	Status          enums.UserStatus   `json:"status" bun:"status,notnull"`
	LastActiveAt    *time.Time         `json:"last_active_at" bun:"last_active_at"`
	DeletedAt       *time.Time         `json:"deleted_at" bun:"deleted_at"`
	Name            string             `json:"name" bun:"name,notnull"`
}

// NewUser permits an empty initial name for passwordless onboarding.
// Rename requires a nonempty name when the user completes their profile.
func NewUser(
	name string,
	email valueobjects.Email,
) (*User, error) {
	name = strings.TrimSpace(name)

	if utf8.RuneCountInString(name) > 100 || strings.ContainsAny(name, "\r\n") {
		return nil, ErrInvalidUserName
	}

	if !email.Valid() {
		return nil, ErrInvalidEmail
	}

	entity, err := domain.NewEntity()
	if err != nil {
		return nil, err
	}
	return &User{
		Entity: entity,
		Status: enums.UserActive,
		Name:   name,
		Email:  email,
	}, nil
}

func (u *User) Rename(name string) error {
	name = strings.TrimSpace(name)

	if name == "" || utf8.RuneCountInString(name) > 100 || strings.ContainsAny(name, "\r\n") {
		return ErrInvalidUserName
	}

	if u.Name == name {
		return nil
	}

	u.Name = name
	u.UpdatedAt = time.Now().UTC()

	return nil
}

// Validate checks persisted account values before writes. It does not establish
// contact ownership. Verification is recorded only after proof by the auth service.
func (u User) Validate() error {
	if u.ID == uuid.Nil || u.CreatedAt.IsZero() || u.UpdatedAt.Before(u.CreatedAt) {
		return ErrInvalidUserState
	}
	if u.Name != strings.TrimSpace(u.Name) || utf8.RuneCountInString(u.Name) > 100 || strings.ContainsAny(u.Name, "\r\n") {
		return ErrInvalidUserName
	}
	if !u.Email.Valid() {
		return ErrInvalidEmail
	}
	if !u.Status.Valid() {
		return ErrInvalidUserStatus
	}
	if u.Phone != nil && !validPhone(*u.Phone) {
		return ErrInvalidPhone
	}
	if u.Phone == nil && u.PhoneVerifiedAt != nil {
		return ErrInvalidContactState
	}
	for _, at := range []*time.Time{u.EmailVerifiedAt, u.PhoneVerifiedAt, u.LastActiveAt, u.DeletedAt} {
		if at != nil && (at.Before(u.CreatedAt) || at.After(u.UpdatedAt)) {
			return ErrInvalidUserState
		}
	}
	if (u.Status == enums.UserDeleted) != (u.DeletedAt != nil) {
		return ErrInvalidUserState
	}
	return nil
}

// ProfileComplete reports whether the required profile fields have been supplied.
// New passwordless accounts may have no name until the user updates their profile.
func (u User) ProfileComplete() bool { return strings.TrimSpace(u.Name) != "" }
