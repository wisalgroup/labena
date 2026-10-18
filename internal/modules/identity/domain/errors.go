// Package domain defines Identity policies and business failures without transport dependencies.
package domain

import "github.com/wisalgroup/labena/pkg/apperror"

var (
	ErrQueueRequired        = apperror.Internal("identity: welcome event handling requires a queue client")
	ErrSubscriberRequired   = apperror.Internal("identity: event routes require a subscriber")
	ErrPhoneTaken           = apperror.Conflict("This phone number is already registered.")
	ErrUserChangeRequired   = apperror.Internal("identity: account change required")
	ErrServiceRequired      = apperror.Internal("identity: service required")
	ErrDependenciesRequired = apperror.Internal("identity: repository and publisher required")
	ErrUserRequired         = apperror.Internal("identity: user required")
	ErrEmailTaken           = apperror.Conflict("This email address is already registered.")
	ErrUserNotFound         = apperror.NotFound("The user was not found.")
)
