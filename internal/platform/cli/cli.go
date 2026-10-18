// Package cli assembles module commands into a Cobra command tree.
// Applications own dependency construction, signal handling, and exit codes.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"text/template"

	"github.com/spf13/cobra"
)

// Config describes the root command and its streams. Nil streams use the process
// standard streams. Version is optional and enables Cobra's --version flag.
type Config struct {
	Use     string
	Short   string
	Version string
	In      io.Reader
	Out     io.Writer
	Err     io.Writer
}

// New validates and attaches fresh, unparented module commands to a root command.
// It does not open dependencies or start services. The caller owns the returned
// tree and should construct a fresh one per invocation; Cobra trees are mutable
// and must not execute concurrently. Errors are returned without being printed.
func New(cfg Config, commands ...*cobra.Command) (*cobra.Command, error) {
	if strings.TrimSpace(cfg.Use) == "" {
		cfg.Use = "cli"
	}
	if cfg.In == nil {
		cfg.In = os.Stdin
	}
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	if cfg.Err == nil {
		cfg.Err = os.Stderr
	}
	seen := make(map[*cobra.Command]bool)
	if err := validate(commands, nil, true, seen); err != nil {
		return nil, err
	}
	root := &cobra.Command{
		Use: cfg.Use, Short: cfg.Short, Version: cfg.Version,
		SilenceErrors: true, SilenceUsage: true, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.SetIn(cfg.In)
	root.SetOut(cfg.Out)
	root.SetErr(cfg.Err)
	root.AddCommand(commands...)
	// Only the root expands descendants. Keep Cobra's native usage for individual
	// commands, including their arguments, examples and inherited flags.
	usage := root.UsageFunc()
	root.SetUsageFunc(func(cmd *cobra.Command) error {
		if cmd != root {
			return usage(cmd)
		}
		return rootUsage.Execute(cmd.OutOrStderr(), cmd)
	})
	return root, nil
}

var rootUsage = template.Must(template.New("root usage").Funcs(template.FuncMap{
	"commands": commandList,
	"trim":     func(s string) string { return strings.TrimRight(s, " \t\r\n") },
}).Parse(`Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if .Aliases}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:
{{commands .}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trim}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trim}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{.Name}}  {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`))

// Read the final tree at help time, including module registrations and Cobra's
// built-in commands. Hidden or deprecated branches retain native help semantics.
func commandList(root *cobra.Command) string {
	var output strings.Builder
	columns := tabwriter.NewWriter(&output, 0, 4, 2, ' ', 0)
	var visit func(*cobra.Command)
	visit = func(parent *cobra.Command) {
		for _, cmd := range parent.Commands() {
			if cmd.Hidden || cmd.Deprecated != "" || (!cmd.IsAvailableCommand() && cmd.Name() != "help") {
				continue
			}
			path := strings.TrimPrefix(cmd.CommandPath(), root.CommandPath()+" ")
			fmt.Fprintf(columns, "  %s\t%s\n", path, cmd.Short)
			visit(cmd)
		}
	}
	visit(root)
	columns.Flush() // strings.Builder writes cannot fail.
	return strings.TrimSuffix(output.String(), "\n")
}

// Validate checks a completed command tree after modules have added commands.
// Call it before the first execution: Cobra adds its own help and completion
// commands during execution. Validation does not execute commands or attach them.
func Validate(root *cobra.Command) error {
	if root == nil {
		return errors.New("cli: nil root command")
	}
	if root.Parent() != nil {
		return errors.New("cli: root command already has a parent")
	}
	return validate(root.Commands(), root, true, map[*cobra.Command]bool{root: true})
}

func validate(commands []*cobra.Command, parent *cobra.Command, top bool, seen map[*cobra.Command]bool) error {
	names := make(map[string]bool)
	if top {
		for _, name := range []string{"help", "completion", "__complete", "__completeNoDesc"} {
			names[name] = true
		}
	}
	for _, cmd := range commands {
		if cmd == nil {
			return errors.New("cli: nil command")
		}
		if seen[cmd] {
			return errors.New("cli: command reused in tree")
		}
		seen[cmd] = true
		if cmd.Parent() != parent {
			return fmt.Errorf("cli: command %q has an unexpected parent", cmd.Name())
		}
		for _, name := range append([]string{cmd.Name()}, cmd.Aliases...) {
			if name == "" || strings.ContainsAny(name, " \t\r\n") || strings.HasPrefix(name, "-") {
				return fmt.Errorf("cli: invalid command name or alias %q", name)
			}
			if names[name] {
				return fmt.Errorf("cli: duplicate or reserved command name %q", name)
			}
			names[name] = true
		}
		if err := validate(cmd.Commands(), cmd, false, seen); err != nil {
			return err
		}
	}
	return nil
}
