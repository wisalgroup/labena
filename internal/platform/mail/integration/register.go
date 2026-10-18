// Package integration registers the mail capability with application transports.
package integration

import (
	"github.com/wisalgroup/labena/internal/platform/dependencies"
	"github.com/wisalgroup/labena/internal/platform/mail"
)

// Register installs the mail task handler before workers start. A nil task mux
// omits queued delivery; the mailer can still send immediately. No I/O occurs.
func Register(sender mail.Sender, targets dependencies.Targets) error {
	handler, err := mail.NewHandler(sender)
	if err != nil {
		return err
	}
	if targets.TaskRouter != nil {
		targets.TaskRouter.Handle(mail.SendTask, handler)
	}
	return nil
}
