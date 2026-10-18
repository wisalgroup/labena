// Package seeders registers named seed operations for explicit console execution.
// Seeders are not migrations: they have no version history and never run at startup.
package seeders

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
)

var validName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)

// Registry's zero value is ready to use. Register during sequential module
// assembly, then treat the registry as immutable. Seeders borrow dependencies
// and must join their work before returning. They own their transaction scope
// and must be safe to rerun; the registry provides neither retries nor rollback.
type Registry struct {
	operations map[string]func(context.Context) error
	names      []string
}

// Register records an operation without executing it. Names must be unique;
// prefer module-qualified names such as orders.example.
func (r *Registry) Register(name string, run func(context.Context) error) error {
	if !validName.MatchString(name) || run == nil {
		return errors.New("seeders: a valid name and operation are required")
	}
	if _, exists := r.operations[name]; exists {
		return fmt.Errorf("seeders: %q already registered", name)
	}
	if r.operations == nil {
		r.operations = make(map[string]func(context.Context) error)
	}
	r.operations[name] = run
	r.names = append(r.names, name)
	return nil
}

// Names returns an independent slice in registration order.
func (r *Registry) Names() []string { return slices.Clone(r.names) }

// Run validates the entire selection before executing it sequentially in the
// requested order. It stops on the first error or cancellation. Already completed
// operations are not undone. No selection is an error; callers must be explicit.
func (r *Registry) Run(ctx context.Context, names ...string) error {
	if len(names) == 0 {
		return errors.New("seeders: select at least one seeder")
	}
	seen := make(map[string]bool)
	for _, name := range names {
		if _, ok := r.operations[name]; !ok {
			return fmt.Errorf("seeders: unknown seeder %q", name)
		}
		if seen[name] {
			return fmt.Errorf("seeders: duplicate selection %q", name)
		}
		seen[name] = true
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.operations[name](ctx); err != nil {
			return fmt.Errorf("seeders: %s: %w", name, err)
		}
	}
	return ctx.Err()
}
