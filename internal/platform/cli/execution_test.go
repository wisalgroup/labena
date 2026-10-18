package cli_test

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/pkg/logging"
)

func TestConsoleNativeHooksFlagsAliasesAndStreamsRunOnce(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var output, diagnostics bytes.Buffer
	var seen []string
	root, err := cli.New(cli.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var label string
	command := &cobra.Command{Use: "probe", Aliases: []string{"p"}, Args: cobra.NoArgs,
		PreRunE: func(*cobra.Command, []string) error { seen = append(seen, "pre"); return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			if logging.FromContext(cmd.Context()) != logger {
				t.Fatal("command lost the logger")
			}
			seen = append(seen, label)
			fmt.Fprint(cmd.ErrOrStderr(), "diagnostic")
			_, err := io.Copy(cmd.OutOrStdout(), cmd.InOrStdin())
			return err
		},
		PostRunE: func(*cobra.Command, []string) error { seen = append(seen, "post"); return nil },
	}
	command.Flags().StringVar(&label, "label", "", "label")
	if err := command.MarkFlagRequired("label"); err != nil {
		t.Fatal(err)
	}
	root.AddCommand(command)
	if err := cli.Validate(root); err != nil {
		t.Fatal(err)
	}
	root.SetIn(strings.NewReader("original input"))
	root.SetOut(&output)
	root.SetErr(&diagnostics)
	root.SetArgs([]string{"p", "--label=action"})
	if err := root.ExecuteContext(logging.WithLogger(t.Context(), logger)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, []string{"pre", "action", "post"}) {
		t.Fatal(seen)
	}
	if output.String() != "original input" || diagnostics.String() != "diagnostic" {
		t.Fatalf("streams changed: %q %q", output.String(), diagnostics.String())
	}
}
