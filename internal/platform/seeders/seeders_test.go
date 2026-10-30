package seeders_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/wisalgroup/labena/internal/platform/seeders"
)

func TestRegistryExecution(t *testing.T) {
	var registry seeders.Registry
	var called []string
	want := errors.New("seed failure")
	for _, name := range []string{"orders", "products", "failed"} {
		if err := registry.RegisterFunc(name, func(context.Context) error {
			called = append(called, name)
			if name == "failed" {
				return want
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if called != nil {
		t.Fatal("registration executed seeders")
	}
	names := registry.Names()
	names[0] = "mutated"
	if !reflect.DeepEqual(registry.Names(), []string{"orders", "products", "failed"}) {
		t.Fatal("names are shared")
	}
	for _, names := range [][]string{nil, {"orders", "unknown"}, {"orders", "orders"}} {
		if err := registry.Run(context.Background(), names...); err == nil {
			t.Fatal("accepted invalid selection", names)
		}
	}
	if called != nil {
		t.Fatal("invalid selection caused partial execution")
	}
	if err := registry.Run(context.Background(), "products", "orders"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(called, []string{"products", "orders"}) {
		t.Fatal(called)
	}
	called = nil
	if err := registry.Run(context.Background(), "orders", "failed", "products"); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(called, []string{"orders", "failed"}) {
		t.Fatal("did not stop on failure", called)
	}
}

func TestValidationAndCancellation(t *testing.T) {
	var registry seeders.Registry
	for _, name := range []string{"", "bad name", "../path"} {
		if err := registry.RegisterFunc(name, func(context.Context) error { return nil }); err == nil {
			t.Fatal("accepted", name)
		}
	}
	if err := registry.RegisterFunc("nil", nil); err == nil {
		t.Fatal("accepted nil operation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := 0
	if err := registry.RegisterFunc("first", func(got context.Context) error {
		if got != ctx {
			t.Error("wrong context")
		}
		called++
		cancel()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterFunc("first", func(context.Context) error { return nil }); err == nil {
		t.Fatal("accepted duplicate")
	}
	if err := registry.RegisterFunc("second", func(context.Context) error { called++; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := registry.Run(ctx, "first", "second"); !errors.Is(err, context.Canceled) || called != 1 {
		t.Fatal(called, err)
	}
	if err := registry.Run(ctx, "first"); !errors.Is(err, context.Canceled) || called != 1 {
		t.Fatal(called, err)
	}
}

// namedOperation lets the registry tests mix both public registration forms.
type namedOperation struct {
	name string
	run  func(context.Context) error
}

func (s namedOperation) Name() string                   { return s.name }
func (s namedOperation) Seed(ctx context.Context) error { return s.run(ctx) }

func TestMixedRegistration(t *testing.T) {
	var registry seeders.Registry
	var calls []string
	ctx := context.Background()
	want := errors.New("seed failed")
	operation := namedOperation{"catalog.defaults", func(got context.Context) error {
		if got != ctx {
			t.Fatal("context was replaced")
		}
		calls = append(calls, "defaults")
		return want
	}}
	if err := registry.Register(operation); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterFunc("catalog.demo", func(context.Context) error {
		calls = append(calls, "demo")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatal("registration executed operations")
	}
	if err := registry.RegisterFunc("catalog.defaults", operation.Seed); err == nil {
		t.Fatal("accepted duplicate object name")
	}
	if err := registry.Register(namedOperation{"catalog.demo", operation.Seed}); err == nil {
		t.Fatal("accepted duplicate function name")
	}
	if !reflect.DeepEqual(registry.Names(), []string{"catalog.defaults", "catalog.demo"}) {
		t.Fatal(registry.Names())
	}
	if err := registry.Run(ctx, "catalog.demo", "catalog.defaults"); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"demo", "defaults"}) {
		t.Fatal(calls)
	}
}

func TestInvalidSeeder(t *testing.T) {
	var registry seeders.Registry
	if err := registry.Register(nil); err == nil {
		t.Fatal("accepted nil seeder")
	}
	if err := registry.Register(namedOperation{"bad name", func(context.Context) error { return nil }}); err == nil {
		t.Fatal("accepted invalid seeder name")
	}
	if len(registry.Names()) != 0 {
		t.Fatal("invalid registration mutated names")
	}
}
