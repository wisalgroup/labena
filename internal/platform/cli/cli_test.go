package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/pkg/logging"
)

func TestContextStreamsAndReturnedErrors(t *testing.T) {
	var out, stderr bytes.Buffer
	want := errors.New("command failed")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(logging.WithLogger(context.Background(), logger))
	cancel()
	cmd := &cobra.Command{Use: "inspect", RunE: func(cmd *cobra.Command, _ []string) error {
		if !errors.Is(cmd.Context().Err(), context.Canceled) {
			t.Error("cancellation not propagated")
		}
		if logging.FromContext(cmd.Context()) != logger {
			t.Error("logger not propagated")
		}
		body, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return err
		}
		if _, err := cmd.OutOrStdout().Write(body); err != nil {
			return err
		}
		return want
	}}
	root, err := cli.New(cli.Config{In: strings.NewReader("input"), Out: &out, Err: &stderr}, cmd)
	if err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"inspect"})
	if err := root.ExecuteContext(ctx); !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	if out.String() != "input" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", out.String(), stderr.String())
	}
}

func TestHelpCompletionAndArgumentValidation(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"help", "task"}, {"completion", "bash"}, {"--version"}, {"task", "--help"}, {"unknown"}, {"task", "extra"}, {"task", "--unknown"}, {"task"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			called := false
			cmd := &cobra.Command{Use: "task", Short: "Example task", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { called = true; return nil }}
			root, err := cli.New(cli.Config{Version: "1.0", Out: &out, Err: &out}, cmd)
			if err != nil {
				t.Fatal(err)
			}
			root.SetArgs(args)
			err = root.ExecuteContext(context.Background())
			invalid := len(args) > 0 && (args[0] == "unknown" || len(args) > 1 && (args[1] == "extra" || args[1] == "--unknown"))
			if (err != nil) != invalid {
				t.Fatalf("error=%v", err)
			}
			expectedCall := len(args) == 1 && args[0] == "task"
			if called != expectedCall {
				t.Fatalf("called=%v", called)
			}
			if !invalid && !expectedCall && out.Len() == 0 {
				t.Fatal("missing output")
			}
		})
	}
}

func TestRootHelpListsNestedCommandsAfterRegistration(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output, diagnostics bytes.Buffer
			root, err := cli.New(cli.Config{Use: "console", Out: &output, Err: &diagnostics})
			if err != nil {
				t.Fatal(err)
			}
			called := false
			action := func(*cobra.Command, []string) { called = true }
			orders := &cobra.Command{Use: "orders", Short: "Manage orders"}
			reports := &cobra.Command{Use: "reports", Short: "Order reports"}
			reports.AddCommand(&cobra.Command{Use: "daily [date]", Short: "Show a daily report", Aliases: []string{"today"}, Run: action})
			hidden := &cobra.Command{Use: "private", Hidden: true}
			hidden.AddCommand(&cobra.Command{Use: "secret", Run: action})
			orders.AddCommand(reports, hidden, &cobra.Command{Use: "old", Deprecated: "use reports", Run: action})
			root.AddCommand(orders)
			root.SetArgs(args)
			if err := root.ExecuteContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			text := output.String()
			for _, expected := range []string{"orders reports daily", "Show a daily report", "completion bash", "completion zsh", "Flags:", "--help"} {
				if !strings.Contains(text, expected) {
					t.Fatalf("missing %q in help:\n%s", expected, text)
				}
			}
			for _, hidden := range []string{"private", "secret", "orders old", "orders reports today", "__complete"} {
				if strings.Contains(text, hidden) {
					t.Fatalf("unexpected %q in help:\n%s", hidden, text)
				}
			}
			if called || diagnostics.Len() != 0 {
				t.Fatalf("help ran an action or printed diagnostics: called=%v stderr=%q", called, diagnostics.String())
			}
		})
	}
}

func TestNestedHelpKeepsNativeArgumentsExamplesAndFlags(t *testing.T) {
	var output bytes.Buffer
	root, err := cli.New(cli.Config{Out: &output})
	if err != nil {
		t.Fatal(err)
	}
	root.PersistentFlags().String("region", "", "Target region")
	group := &cobra.Command{Use: "orders"}
	command := &cobra.Command{Use: "show ID", Aliases: []string{"inspect"}, Example: "cli orders show 123", Run: func(*cobra.Command, []string) { t.Fatal("help executed command") }}
	command.Flags().Bool("json", false, "Print JSON")
	group.AddCommand(command)
	root.AddCommand(group)
	root.SetArgs([]string{"help", "orders", "show"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"cli orders show ID [flags]", "Aliases:", "inspect", "Examples:", "cli orders show 123", "--json", "Global Flags:", "--region"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("missing %q in help:\n%s", expected, output.String())
		}
	}
}

func TestRejectInvalidTreesBeforeAttaching(t *testing.T) {
	cases := map[string][]*cobra.Command{
		"nil":             {nil},
		"empty":           {{}},
		"duplicate":       {{Use: "task"}, {Use: "task"}},
		"alias collision": {{Use: "one", Aliases: []string{"two"}}, {Use: "two"}},
		"reserved":        {{Use: "completion"}},
		"reserved alias":  {{Use: "task", Aliases: []string{"help"}}},
		"invalid alias":   {{Use: "task", Aliases: []string{"two words"}}},
	}
	group := &cobra.Command{Use: "group"}
	group.AddCommand(&cobra.Command{Use: "duplicate"}, &cobra.Command{Use: "duplicate"})
	cases["nested collision"] = []*cobra.Command{group}
	parent := &cobra.Command{Use: "parent"}
	child := &cobra.Command{Use: "child"}
	parent.AddCommand(child)
	cases["already attached"] = []*cobra.Command{child}
	for name, commands := range cases {
		t.Run(name, func(t *testing.T) {
			valid := &cobra.Command{Use: "valid"}
			commands = append([]*cobra.Command{valid}, commands...)
			if _, err := cli.New(cli.Config{}, commands...); err == nil {
				t.Fatal("accepted invalid tree")
			}
			if valid.Parent() != nil {
				t.Fatal("partial registration on failure")
			}
		})
	}
}

func TestValidateAfterModuleRegistration(t *testing.T) {
	for _, tt := range []struct {
		name    string
		add     func(*cobra.Command)
		invalid bool
	}{
		{"valid module", func(root *cobra.Command) {
			group := &cobra.Command{Use: "orders"}
			group.AddCommand(&cobra.Command{Use: "reconcile"})
			root.AddCommand(group)
		}, false},
		{"builtin collision", func(root *cobra.Command) {
			root.AddCommand(&cobra.Command{Use: "serve"})
		}, true},
		{"reserved alias", func(root *cobra.Command) {
			root.AddCommand(&cobra.Command{Use: "orders", Aliases: []string{"help"}})
		}, true},
		{"nested collision", func(root *cobra.Command) {
			group := &cobra.Command{Use: "orders"}
			group.AddCommand(&cobra.Command{Use: "one", Aliases: []string{"two"}}, &cobra.Command{Use: "two"})
			root.AddCommand(group)
		}, true},
		{"reused command", func(root *cobra.Command) {
			command := &cobra.Command{Use: "orders"}
			root.AddCommand(command, command)
		}, true},
		{"reparented command", func(root *cobra.Command) {
			first, second := &cobra.Command{Use: "one"}, &cobra.Command{Use: "two"}
			child := &cobra.Command{Use: "child"}
			first.AddCommand(child)
			second.AddCommand(child)
			root.AddCommand(first)
		}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			builtin := &cobra.Command{Use: "serve", Run: func(*cobra.Command, []string) { called = true }}
			root, err := cli.New(cli.Config{}, builtin)
			if err != nil {
				t.Fatal(err)
			}
			tt.add(root)
			if err := cli.Validate(root); (err != nil) != tt.invalid {
				t.Fatalf("Validate()=%v, invalid=%v", err, tt.invalid)
			}
			if called {
				t.Fatal("validation executed a command")
			}
		})
	}
	if err := cli.Validate(nil); err == nil {
		t.Fatal("accepted nil root")
	}
	parent, child := &cobra.Command{Use: "parent"}, &cobra.Command{Use: "child"}
	parent.AddCommand(child)
	if err := cli.Validate(child); err == nil {
		t.Fatal("accepted child as root")
	}
}
