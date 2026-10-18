package domain

import "github.com/wisalgroup/labena/pkg/apperror"

var (
	ErrSocialInvalid      = apperror.Unauthenticated("The social sign-in is invalid or expired. Start again.")
	ErrSocialProvider     = apperror.Validation("This sign-in provider is not enabled.")
	ErrSocialLinkRequired = apperror.Conflict("Sign in using your existing account. This provider is not linked to it.")
	ErrSocialEmail        = apperror.Validation("Use email sign-in to verify this email address.")
)

// SocialIdentity contains only claims verified by the provider integration.
// Subject is the stable account key. Email is never authority to link accounts.
type SocialIdentity struct {
	Subject       string
	Email         string
	Name          string
	EmailVerified bool
}
