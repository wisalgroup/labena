// Labena creates independent applications from a versioned template.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/cmd/labena/internal/scaffold"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := command().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "labena:", err)
		os.Exit(1)
	}
}

func command() *cobra.Command {
	version := "devel"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		version = info.Main.Version
	}
	root := &cobra.Command{Use: "labena", Short: "Create an application with Labena", Version: version, SilenceErrors: true, SilenceUsage: true}
	root.CompletionOptions.DisableDefaultCmd = true
	var options scaffold.Options
	create := &cobra.Command{
		Use: "new directory --module path --name name", Short: "Create a new, independent Go application",
		Args:    cobra.ExactArgs(1),
		Example: "  labena new myapp --module github.com/your-org/myapp --name MyApp",
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Directory = args[0]
			if options.Source == "" && options.Version == "" {
				if version == "devel" || version == "(devel)" || strings.Contains(version, "+dirty") {
					return fmt.Errorf("development builds need --source /path/to/labena or --template-version vX.Y.Z")
				}
				options.Version = version
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Creating application…")
			if err := scaffold.Create(cmd.Context(), options); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s in %s.\nFollow docs/getting-started.md to start PostgreSQL and Redis, then run migrations and the app.\n", options.Name, options.Directory)
			return nil
		},
	}
	create.Flags().StringVar(&options.Module, "module", "", "Go module path for the new application (required)")
	create.Flags().StringVar(&options.Name, "name", "", "Application display name (required)")
	create.Flags().StringVar(&options.Source, "source", "", "Use a local Labena checkout, including uncommitted source changes")
	create.Flags().StringVar(&options.Version, "template-version", "", "Use this exact template version instead of the installed CLI version")
	create.Flags().BoolVar(&options.SkipChecks, "skip-checks", false, "Skip dependency tidy, documentation generation and build checks")
	_ = create.MarkFlagRequired("module")
	_ = create.MarkFlagRequired("name")
	create.MarkFlagsMutuallyExclusive("source", "template-version")
	root.AddCommand(create)
	return root
}
