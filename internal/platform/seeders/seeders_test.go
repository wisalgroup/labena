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
		if err := registry.Register(name, func(context.Context) error {
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
		if err := registry.Register(name, func(context.Context) error { return nil }); err == nil {
			t.Fatal("accepted", name)
		}
	}
	if err := registry.Register("nil", nil); err == nil {
		t.Fatal("accepted nil operation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := 0
	if err := registry.Register("first", func(got context.Context) error {
		if got != ctx {
			t.Error("wrong context")
		}
		called++
		cancel()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("first", func(context.Context) error { return nil }); err == nil {
		t.Fatal("accepted duplicate")
	}
	if err := registry.Register("second", func(context.Context) error { called++; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := registry.Run(ctx, "first", "second"); !errors.Is(err, context.Canceled) || called != 1 {
		t.Fatal(called, err)
	}
	if err := registry.Run(ctx, "first"); !errors.Is(err, context.Canceled) || called != 1 {
		t.Fatal(called, err)
	}
}
