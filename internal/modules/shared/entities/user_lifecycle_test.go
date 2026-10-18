package entities

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
)

func account(t *testing.T) *User {
	t.Helper()
	email, _ := valueobjects.NewEmail("first@example.com")
	u, err := NewUser("Test", email)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestUserStatusTransitions(t *testing.T) {
	for _, from := range enums.UserStatusValues() {
		for _, to := range enums.UserStatusValues() {
			t.Run(string(from)+"/"+string(to), func(t *testing.T) {
				u := account(t)
				u.Status = from
				if from == enums.UserDeleted {
					v := u.CreatedAt
					u.DeletedAt = &v
				}
				before := *u
				allowed := from == to || (from == enums.UserActive && (to == enums.UserSuspended || to == enums.UserPendingDeletion)) || (from == enums.UserSuspended && (to == enums.UserActive || to == enums.UserPendingDeletion)) || (from == enums.UserPendingDeletion && to == enums.UserDeleted)
				err := u.ChangeStatus(to, u.UpdatedAt.Add(time.Second))
				if !allowed {
					if !errors.Is(err, ErrUserTransition) || !reflect.DeepEqual(before, *u) {
						t.Fatal("rejected transition mutated account", err)
					}
					return
				}
				if err != nil || u.Status != to {
					t.Fatal(err)
				}
				if err = u.Validate(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestUserContactVerification(t *testing.T) {
	u := account(t)
	now := u.UpdatedAt.Add(time.Second)
	if u.Status != enums.UserActive || u.EmailVerifiedAt != nil || u.Phone != nil {
		t.Fatal("unexpected defaults")
	}
	if err := u.VerifyEmail(u.Email, now); err != nil {
		t.Fatal(err)
	}
	old := u.Email
	next, _ := valueobjects.NewEmail("second@example.com")
	if err := u.ChangeEmail(next, now); err != nil || u.EmailVerifiedAt != nil {
		t.Fatal("email proof not reset", err)
	}
	if err := u.VerifyEmail(old, now); !errors.Is(err, ErrContactChanged) {
		t.Fatal("stale proof accepted", err)
	}
	phone := "+201001234567"
	if err := u.ChangePhone(&phone, now); err != nil {
		t.Fatal(err)
	}
	phone = "mutated"
	if *u.Phone != "+201001234567" {
		t.Fatal("borrowed mutable phone")
	}
	if err := u.VerifyPhone(*u.Phone, now); err != nil {
		t.Fatal(err)
	}
	if err := u.ChangePhone(nil, now); err != nil || u.PhoneVerifiedAt != nil || u.Phone != nil {
		t.Fatal("phone removal", err)
	}
	for _, raw := range []string{"", "01001234567", "+0123", "+1 2025550123", "+1234567890123456"} {
		before := *u
		if err := u.ChangePhone(&raw, now); !errors.Is(err, ErrInvalidPhone) || !reflect.DeepEqual(before, *u) {
			t.Fatal("invalid phone accepted", raw, err)
		}
	}
	if err := u.ChangeStatus(enums.UserSuspended, now); err != nil {
		t.Fatal(err)
	}
	if err := u.ChangeEmail(old, now); !errors.Is(err, ErrUserInactive) {
		t.Fatal(err)
	}
}
