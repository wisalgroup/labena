package entities

import (
	"time"

	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
)

// ChangeStatus applies the lifecycle transition. Authorization and session revocation
// belong to the service. Deleted is terminal; contacts remain reserved after deletion.
func (u *User) ChangeStatus(status enums.UserStatus, at time.Time) error {
	if err := u.Validate(); err != nil {
		return err
	}
	if !status.Valid() {
		return ErrInvalidUserStatus
	}
	if at.Before(u.UpdatedAt) {
		return ErrInvalidUserState
	}
	if status == u.Status {
		return nil
	}
	allowed := false
	switch u.Status {
	case enums.UserActive:
		allowed = status == enums.UserSuspended || status == enums.UserPendingDeletion
	case enums.UserSuspended:
		allowed = status == enums.UserActive || status == enums.UserPendingDeletion
	case enums.UserPendingDeletion:
		allowed = status == enums.UserDeleted
	}
	if !allowed {
		return ErrUserTransition
	}
	u.Status = status
	u.UpdatedAt = at.UTC()
	if status == enums.UserDeleted {
		v := at.UTC()
		u.DeletedAt = &v
	}
	return nil
}

func (u *User) canChangeContact(at time.Time) error {
	if err := u.Validate(); err != nil {
		return err
	}
	if u.Status != enums.UserActive {
		return ErrUserInactive
	}
	if at.Before(u.UpdatedAt) {
		return ErrInvalidUserState
	}
	return nil
}

// ChangeEmail clears proof for the old email. It does not itself authorize a change.
func (u *User) ChangeEmail(email valueobjects.Email, at time.Time) error {
	if err := u.canChangeContact(at); err != nil {
		return err
	}
	if !email.Valid() {
		return ErrInvalidEmail
	}
	if u.Email.Equal(email) {
		return nil
	}
	u.Email = email
	u.EmailVerifiedAt = nil
	u.UpdatedAt = at.UTC()
	return nil
}

// ChangePhone accepts an already canonical international number, or nil to remove
// it. It copies the input and resets verification only when the number changes.
func (u *User) ChangePhone(phone *string, at time.Time) error {
	if err := u.canChangeContact(at); err != nil {
		return err
	}
	if phone != nil && !validPhone(*phone) {
		return ErrInvalidPhone
	}
	if (phone == nil && u.Phone == nil) || (phone != nil && u.Phone != nil && *phone == *u.Phone) {
		return nil
	}
	u.Phone = nil
	if phone != nil {
		v := *phone
		u.Phone = &v
	}
	u.PhoneVerifiedAt = nil
	u.UpdatedAt = at.UTC()
	return nil
}

// VerifyEmail must only follow successful challenge verification. Matching the
// destination prevents an old challenge from verifying a replacement contact.
func (u *User) VerifyEmail(email valueobjects.Email, at time.Time) error {
	if err := u.canChangeContact(at); err != nil {
		return err
	}
	if !email.Valid() || !u.Email.Equal(email) {
		return ErrContactChanged
	}
	if u.EmailVerifiedAt != nil {
		return nil
	}
	v := at.UTC()
	u.EmailVerifiedAt = &v
	u.UpdatedAt = v
	return nil
}

// VerifyPhone must only follow proof of control of the current number.
func (u *User) VerifyPhone(phone string, at time.Time) error {
	if err := u.canChangeContact(at); err != nil {
		return err
	}
	if u.Phone == nil || *u.Phone != phone {
		return ErrContactChanged
	}
	if u.PhoneVerifiedAt != nil {
		return nil
	}
	v := at.UTC()
	u.PhoneVerifiedAt = &v
	u.UpdatedAt = v
	return nil
}

// validPhone checks E.164-shaped input, not assignment, reachability, or ownership.
func validPhone(phone string) bool {
	if len(phone) < 3 || len(phone) > 16 || phone[0] != '+' || phone[1] < '1' || phone[1] > '9' {
		return false
	}
	for i := 2; i < len(phone); i++ {
		if phone[i] < '0' || phone[i] > '9' {
			return false
		}
	}
	return true
}
