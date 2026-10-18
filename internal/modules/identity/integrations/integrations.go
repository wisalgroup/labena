package integrations

import (
	"context"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
)

// SocialProvider verifies an external credential and returns trusted identity claims.
// Implementations borrow their clients and must support concurrent use.
type SocialProvider interface {
	Verify(context.Context, string) (domain.SocialIdentity, error)
}
