package cli

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
)

func New(users service.UserService) *cobra.Command {
	group := &cobra.Command{Use: "identity", Short: "Identity commands"}
	group.AddCommand(&cobra.Command{
		Use: "show USER_ID", Short: "Show an account", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userID, err := uuid.Parse(args[0])
			if err != nil {
				return err
			}
			user, err := users.Get(cmd.Context(), userID)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(user)
		},
	})
	return group
}
