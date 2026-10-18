package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/spf13/cobra"
)

func TestScheduledCommandsIsolationAndValidation(t *testing.T) {
	mux := asynq.NewServeMux()
	commands := &ScheduledCommands{}
	mux.Handle(CommandTask, commands)
	for _, task := range []*asynq.Task{nil, asynq.NewTask(CommandTask+".unknown", nil), asynq.NewTask(CommandTask, []byte("null"))} {
		if err := commands.ProcessTask(t.Context(), task); !errors.Is(err, asynq.SkipRetry) {
			t.Fatal("invalid task did not skip retries", err)
		}
	}
	got := make(chan string, 2)
	if err := commands.Register("example", func() *cobra.Command {
		var value string
		c := &cobra.Command{Use: "example", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			got <- value
			_, err := fmt.Fprintln(cmd.OutOrStdout(), value)
			return err
		}}
		c.Flags().StringVar(&value, "value", "default", "test")
		return c
	}); err != nil {
		t.Fatal(err)
	}
	if err := commands.Register("example", func() *cobra.Command { return nil }); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := commands.Task("unregistered"); err == nil {
		t.Fatal("unregistered command accepted")
	}
	var wg sync.WaitGroup
	for _, args := range [][]string{{"--value=custom"}, {}} {
		task, err := commands.Task("example", args...)
		if err != nil {
			t.Fatal(err)
		}
		wg.Go(func() {
			if err := mux.ProcessTask(t.Context(), asynq.NewTask(task.Type(), task.Payload())); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	values := map[string]bool{<-got: true, <-got: true}
	if !values["custom"] || !values["default"] {
		t.Fatal("flag state leaked", values)
	}
	if err := mux.ProcessTask(t.Context(), asynq.NewTask(CommandTask, []byte(`{"name":"missing"}`))); !errors.Is(err, asynq.SkipRetry) {
		t.Fatal("unknown command not archived", err)
	}
	invalid, _ := commands.Task("example", "--unknown")
	if err := mux.ProcessTask(t.Context(), asynq.NewTask(invalid.Type(), invalid.Payload())); err == nil {
		t.Fatal("invalid flag accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := mux.ProcessTask(ctx, asynq.NewTask(CommandTask, []byte(`{"name":"example"}`))); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestScheduledCommandOutputBound(t *testing.T) {
	output := &commandOutput{}
	if _, err := io.WriteString(output, strings.Repeat("x", 64*1024+1)); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	if !output.overflow || output.buffer.Len() != 0 {
		t.Fatal("unbounded output")
	}
	mux := asynq.NewServeMux()
	commands := &ScheduledCommands{}
	mux.Handle(CommandTask, commands)
	if err := commands.Register("large", func() *cobra.Command {
		return &cobra.Command{Use: "large", RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = io.WriteString(cmd.OutOrStdout(), strings.Repeat("x", 64*1024+1))
			return nil
		}}
	}); err != nil {
		t.Fatal(err)
	}
	task, _ := commands.Task("large")
	if err := mux.ProcessTask(t.Context(), asynq.NewTask(task.Type(), task.Payload())); err == nil {
		t.Fatal("ignored writer failure lost")
	}
}
