// A complete console application and module command in one file.
// Run: go run ./examples/cli greet --name Ada
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/pkg/logging"
)

// In an expanded module, add commands in Register and keep each command
// constructor in its own delivery/cli file. This standalone example uses
// cli.New directly and stays in one file.
func commands() []*cobra.Command { return []*cobra.Command{newGreetCommand()} }

func newGreetCommand() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use: "greet", Short: "Print a greeting", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := ctx.Err(); err != nil {
				return err
			}
			logging.FromContext(ctx).InfoContext(ctx, "greeting requested", "name", name)
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Hello, %s!\n", name)
			return err
		},
	}
	cmd.Flags().StringVar(&name, "name", "world", "Name to greet")
	return cmd
}

func run() error {
	logger, err := logging.New(os.Stderr, logging.Options{})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root, err := cli.New(cli.Config{Use: "example", Short: "Console example"}, commands()...)
	if err != nil {
		return err
	}
	return root.ExecuteContext(logging.WithLogger(ctx, logger))
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
