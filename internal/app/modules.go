package app

import (
	"fmt"

	"github.com/wisalgroup/labena/internal/modules/identity"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
)

// registerModules lists business registrations in dependency order.
// App translates settings and passes business collaborators explicitly.
func (a *application) registerModules(authentication service.AuthService) error {
	if err := identity.Register(a.resources, a.targets, authentication, a.cfg.Auth.GoogleEnabled); err != nil {
		return fmt.Errorf("register identity: %w", err)
	}
	return nil
}
