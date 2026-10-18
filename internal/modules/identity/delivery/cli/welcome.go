package cli

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/jobs"
	"github.com/wisalgroup/labena/pkg/asynqx"
)

// NewWelcome gives operators an explicit way to recover a missed signup event.
// Reusing the command does not create a new notification occurrence.
func NewWelcome(queue asynqx.Client) *cobra.Command {
	return &cobra.Command{Use: "welcome USER_ID", Short: "Queue or recover an account welcome notification", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		task, err := jobs.NewWelcome(id)
		if err != nil {
			return err
		}
		if queue == nil {
			return domain.ErrQueueRequired
		}
		_, err = queue.EnqueueContext(cmd.Context(), task)
		return err
	}}
}
