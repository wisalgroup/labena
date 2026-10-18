package events

import "github.com/google/uuid"

// UserRegisteredType is the topic for Identity account creation.
const UserRegisteredType = "identity.user_registered.v1"

// UserRegistered records an account created by Identity after its transaction
// commits. Consuming modules own their reactions.
type UserRegistered struct {
	Event

	UserID uuid.UUID `json:"user_id"`
}

// NewUserRegistered describes a new account occurrence without publishing it.
func NewUserRegistered(userID uuid.UUID) UserRegistered {
	return UserRegistered{
		Event:  NewEvent(UserRegisteredType),
		UserID: userID,
	}
}
