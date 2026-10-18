package cli

import (
	"encoding/json"

	"github.com/hibiken/asynq"
	"github.com/spf13/cobra"
)

// NewQueueCommand builds native Asynq administration commands without I/O.
// Administrative commands are opt-in. They borrow the same bounded Redis client
// as workers and are intentionally absent from public HTTP routes.
// The inspector must be non-nil and remain open until command execution finishes.
func NewQueueCommand(inspector *asynq.Inspector) *cobra.Command {
	root := &cobra.Command{Use: "queue", Short: "Inspect and administer Asynq tasks"}
	root.AddCommand(&cobra.Command{Use: "list", Short: "List queue names", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		names, err := inspector.Queues()
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(names)
	}})
	root.AddCommand(&cobra.Command{Use: "inspect QUEUE ID", Short: "Show a task, its state and retained result", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		info, err := inspector.GetTaskInfo(args[0], args[1])
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(info)
	}})
	for _, op := range []struct {
		use, description string
		run              func(*asynq.Inspector, string, string) error
	}{
		{"replay", "Move a scheduled, retry or archived task to pending", (*asynq.Inspector).RunTask},
		{"delete", "Delete a non-active task permanently", (*asynq.Inspector).DeleteTask},
		{"archive", "Archive a non-active task", (*asynq.Inspector).ArchiveTask},
	} {
		root.AddCommand(&cobra.Command{Use: op.use + " QUEUE ID", Short: op.description, Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			return op.run(inspector, args[0], args[1])
		}})
	}
	for _, op := range []struct {
		use, description string
		run              func(*asynq.Inspector, string) error
	}{
		{"pause QUEUE", "Pause new processing; active tasks continue", (*asynq.Inspector).PauseQueue},
		{"resume QUEUE", "Resume processing", (*asynq.Inspector).UnpauseQueue},
		{"cancel ID", "Request cooperative cancellation of an active task", (*asynq.Inspector).CancelProcessing},
	} {
		root.AddCommand(&cobra.Command{Use: op.use, Short: op.description, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			return op.run(inspector, args[0])
		}})
	}
	return root
}
