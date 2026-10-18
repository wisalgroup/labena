package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/hibiken/asynq"

	"github.com/spf13/cobra"
	"github.com/wisalgroup/labena/pkg/asynqx"
)

// CommandTask is the persisted task type for allowlisted command invocations.
const CommandTask = "app.command"

type commandPayload struct {
	Name string   `json:"name"`
	Args []string `json:"args,omitempty"`
}

// ScheduledCommands is an explicit allowlist and native Asynq handler. Its zero
// value is ready to use. Install it with mux.Handle(CommandTask, commands). Register
// sequentially during assembly, then treat it as immutable. Each factory must
// return a fresh standalone command with no parent and no shared flag state.
// Commands borrow services, honor context cancellation and never exit the process.
type ScheduledCommands struct {
	factories map[string]func() *cobra.Command
}

func (c *ScheduledCommands) Register(name string, factory func() *cobra.Command) error {
	if strings.TrimSpace(name) == "" || factory == nil {
		return errors.New("cli: command name and factory required")
	}
	if _, exists := c.factories[name]; exists {
		return fmt.Errorf("cli: duplicate scheduled command %q", name)
	}
	if c.factories == nil {
		c.factories = make(map[string]func() *cobra.Command)
	}
	c.factories[name] = factory
	return nil
}

// Task creates a durable command invocation. Arguments are passed as tokens,
// never interpreted by a shell. Name must have been explicitly registered.
func (c *ScheduledCommands) Task(name string, args ...string) (*asynq.Task, error) {
	if _, ok := c.factories[name]; !ok {
		return nil, fmt.Errorf("cli: unregistered scheduled command %q", name)
	}
	payload, err := json.Marshal(commandPayload{name, args})
	if err != nil {
		return nil, err
	}
	return asynqx.NewTask(CommandTask, payload), nil
}

// ProcessTask runs one fresh command instance. The registry is read-only after
// registration, so different invocations can execute concurrently without sharing
// Cobra state. Unknown tasks and malformed payloads skip retries.
func (c *ScheduledCommands) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload *commandPayload
	if task == nil || task.Type() != CommandTask {
		return fmt.Errorf("cli: unknown command task: %w", asynq.SkipRetry)
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload == nil {
		return fmt.Errorf("cli: invalid command payload: %w", asynq.SkipRetry)
	}
	factory, ok := c.factories[payload.Name]
	if !ok {
		return fmt.Errorf("cli: unknown scheduled command: %w", asynq.SkipRetry)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := factory()
	if cmd == nil || cmd.Parent() != nil {
		return errors.New("cli: scheduled factory must return a standalone command")
	}
	if err := Validate(cmd); err != nil {
		return err
	}
	// A fresh tree isolates flags, context, arguments and streams between attempts.
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs(append([]string{}, payload.Args...))
	cmd.SetIn(strings.NewReader(""))
	output := &commandOutput{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	if err := cmd.ExecuteContext(ctx); err != nil {
		return err
	}
	if output.overflow {
		return errors.New("cli: scheduled command output exceeds 64 KiB")
	}
	if task.ResultWriter() != nil && output.buffer.Len() > 0 {
		_, err := task.ResultWriter().Write(output.buffer.Bytes())
		return err
	}
	return nil
}

// Bound captured output even when commands ignore writer errors. Output is kept
// in the task result, not emitted to operational logs.
type commandOutput struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	overflow bool
}

func (w *commandOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buffer.Len()+len(p) > 64*1024 {
		w.overflow = true
		return 0, io.ErrShortWrite
	}
	return w.buffer.Write(p)
}
