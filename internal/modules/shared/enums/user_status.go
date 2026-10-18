package enums

import "slices"

// UserStatus is the Identity account lifecycle, independent of contact verification.
type UserStatus string

const (
	UserActive          UserStatus = "active"
	UserSuspended       UserStatus = "suspended"
	UserPendingDeletion UserStatus = "pending_deletion"
	UserDeleted         UserStatus = "deleted"
)

// UserStatusValues returns an independent slice of supported statuses.
func UserStatusValues() []UserStatus {
	return []UserStatus{UserActive, UserSuspended, UserPendingDeletion, UserDeleted}
}
func (v UserStatus) Valid() bool { return slices.Contains(UserStatusValues(), v) }
