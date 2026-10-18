package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
)

// NewPruneVerification returns a fresh bounded command for console or scheduling.
func NewPruneVerification(challenges service.AuthService) *cobra.Command {
	return &cobra.Command{Use: "prune-verification", Short: "Remove up to 1000 expired verification challenges", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			count, err := challenges.PruneVerification(ctx, 1000)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Removed %d expired verification records.\n", count)
			return err
		}}
}
