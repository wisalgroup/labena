package integration

import (
	"encoding/json"
	"os"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/internal/platform/media"
	"github.com/wisalgroup/labena/pkg/pagination"
)

// Operator commands use explicit owners; filesystem paths are local CLI input.
func newCommands(svc *media.Library) *cobra.Command {
	group := &cobra.Command{Use: "media", Short: "Asset operations"}
	var collection, visibility string
	group.PersistentFlags().StringVar(&visibility, "visibility", "", "Upload visibility: public or private (default: disk configuration)")
	group.PersistentFlags().StringVar(&collection, "collection", "", "Upload collection (default: default), or list filter")
	group.AddCommand(&cobra.Command{Use: "upload OWNER_TYPE OWNER_ID FILE", Short: "Upload a file (20 MiB maximum)", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		owner := media.Owner{Type: args[0], ID: args[1]}
		file, err := os.Open(args[2])
		if err != nil {
			return err
		}
		defer file.Close()
		asset, err := svc.Upload(cmd.Context(), media.UploadInput{Visibility: media.Visibility(visibility), Owner: owner, Collection: collection, Filename: file.Name(), Body: file, Policy: media.Policy{MaxBytes: 20 << 20}})
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(asset.ID)
	}})
	group.AddCommand(&cobra.Command{Use: "list OWNER_TYPE OWNER_ID", Short: "List ready assets", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		owner := media.Owner{Type: args[0], ID: args[1]}
		page, err := svc.List(cmd.Context(), owner, collection, pagination.NewPagination(1, 50))
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(page)
	}})
	group.AddCommand(&cobra.Command{Use: "delete OWNER_TYPE OWNER_ID ASSET_ID", Short: "Delete an owned asset", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		owner := media.Owner{Type: args[0], ID: args[1]}
		id, err := uuid.Parse(args[2])
		if err != nil {
			return err
		}
		return svc.Delete(cmd.Context(), owner, id)
	}})
	group.AddCommand(&cobra.Command{Use: "visibility OWNER_TYPE OWNER_ID ASSET_ID VISIBILITY", Short: "Set an owned asset's visibility to public or private", Args: cobra.ExactArgs(4), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[2])
		if err != nil {
			return err
		}
		asset, err := svc.SetVisibility(cmd.Context(), media.Owner{Type: args[0], ID: args[1]}, id, media.Visibility(args[3]))
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(asset)
	}})
	group.AddCommand(&cobra.Command{Use: "recover", Short: "Recover interrupted uploads, visibility changes and deletions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		n, err := svc.Recover(cmd.Context())
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(n)
	}})
	return group
}
