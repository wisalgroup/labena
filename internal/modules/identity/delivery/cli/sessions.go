package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
)

func NewPruneSessions(sessions service.AuthService) *cobra.Command {
	return &cobra.Command{Use: "prune-sessions", Short: "Remove bounded batches of revoked or expired session/token history", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		count, err := sessions.PruneSessions(ctx, 1000)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Removed %d revoked or expired session records.\n", count)
		return err
	}}
}
