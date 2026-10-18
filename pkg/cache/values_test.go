package cache_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wisalgroup/labena/pkg/cache"
	"github.com/wisalgroup/labena/pkg/cache/memory"
)

func TestRememberFailureAndExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		s, _ := memory.New(10, 64)
		calls := 0
		load := func(context.Context) (any, error) { calls++; return "loaded", nil }
		if _, err := cache.Remember[any](ctx, s, "key", 0, load); !errors.Is(err, cache.ErrInvalidTTL) {
			t.Fatal(err)
		}
		if _, err := cache.Remember[any](ctx, s, "", time.Second, load); !errors.Is(err, cache.ErrInvalidKey) {
			t.Fatal(err)
		}
		if _, err := cache.Remember[any](ctx, s, "nil", time.Second, nil); err == nil {
			t.Fatal("nil loader accepted")
		}
		if calls != 0 {
			t.Fatal("invalid input called loader")
		}
		if _, err := cache.Remember[any](ctx, s, "key", time.Second, load); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		if exists, err := s.Exists(ctx, "key"); err != nil || exists {
			t.Fatal(exists, err)
		}
		if _, err := cache.Remember[any](ctx, s, "key", time.Second, load); err != nil {
			t.Fatal(err)
		}
		if calls != 2 {
			t.Fatal(calls)
		}
		if _, err := cache.Remember[any](ctx, s, "long", cache.MaxTTL, load); err != nil {
			t.Fatal(err)
		}
		time.Sleep(365 * 24 * time.Hour)
		if exists, err := s.Exists(ctx, "long"); err != nil || !exists {
			t.Fatal(exists, err)
		}
		if _, err := cache.Remember[any](ctx, s, "unsupported", time.Minute, func(context.Context) (any, error) { return make(chan int), nil }); !errors.Is(err, cache.ErrInvalidValue) {
			t.Fatal(err)
		}
		if _, err := cache.Remember[any](ctx, s, "oversized", time.Minute, func(context.Context) (any, error) { return strings.Repeat("x", 64), nil }); !errors.Is(err, cache.ErrTooLarge) {
			t.Fatal(err)
		}
		child, cancel := context.WithCancel(ctx)
		if _, err := cache.Remember[any](child, s, "canceled-load", time.Minute, func(context.Context) (any, error) { cancel(); return "bad", nil }); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if exists, err := s.Exists(ctx, "canceled-load"); err != nil || exists {
			t.Fatal(exists, err)
		}
		_ = s.Close(ctx)
		before := calls
		if _, err := cache.Remember[any](ctx, s, "closed", time.Minute, load); !errors.Is(err, cache.ErrClosed) {
			t.Fatal(err)
		}
		if calls != before {
			t.Fatal("loaded on backend error")
		}
		if err := s.Clear(ctx); !errors.Is(err, cache.ErrClosed) {
			t.Fatal(err)
		}
		if _, err := cache.GetMulti[any](ctx, s, nil); !errors.Is(err, cache.ErrClosed) {
			t.Fatal(err)
		}
		if err := cache.SetMulti[any](ctx, s, nil, time.Minute); !errors.Is(err, cache.ErrClosed) {
			t.Fatal(err)
		}
	})
}

func TestMemoryBatchCapacityFailure(t *testing.T) {
	s, _ := memory.New(1, 64)
	ctx := context.Background()
	// Runtime admission failures can leave earlier, sorted items cached.
	if err := cache.SetMulti[any](ctx, s, map[string]any{"a": 1, "b": 2}, time.Hour); !errors.Is(err, cache.ErrFull) {
		t.Fatal(err)
	}
	if exists, err := s.Exists(ctx, "a"); err != nil || !exists {
		t.Fatal(exists, err)
	}
	if exists, err := s.Exists(ctx, "b"); err != nil || exists {
		t.Fatal(exists, err)
	}
	if _, err := cache.Remember[any](ctx, s, "b", time.Hour, func(context.Context) (any, error) { return 2, nil }); !errors.Is(err, cache.ErrFull) {
		t.Fatal(err)
	}
	if err := s.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if usage := s.Usage(); usage.Bytes != 0 || usage.Entries != 0 || usage.Closed {
		t.Fatal(usage)
	}
}

func TestBulkPayloadBounds(t *testing.T) {
	ctx := context.Background()
	s, _ := memory.New(2000, 5<<20)
	items := make(map[string]any, cache.MaxBatchKeys+1)
	for i := range cache.MaxBatchKeys + 1 {
		items[fmt.Sprintf("k%d", i)] = true
	}
	if err := cache.SetMulti[any](ctx, s, items, time.Hour); !errors.Is(err, cache.ErrTooLarge) {
		t.Fatal(err)
	}
	payload := strings.Repeat("x", 4<<20)
	if err := cache.SetMulti[any](ctx, s, map[string]any{"a": payload, "b": payload}, time.Hour); !errors.Is(err, cache.ErrTooLarge) {
		t.Fatal(err)
	}
	if usage := s.Usage(); usage.Entries != 0 {
		t.Fatal("oversized batch wrote values", usage)
	}
	encoded, _ := json.Marshal(payload)
	for _, key := range []string{"a", "b"} {
		if err := s.Set(ctx, key, encoded, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if values, err := cache.GetMulti[any](ctx, s, []string{"a", "b"}); !errors.Is(err, cache.ErrTooLarge) || values != nil {
		t.Fatal("oversized result", err)
	}
	for _, invalid := range []string{"", "null null", "{} trailing", "{broken}"} {
		if _, err := cache.DecodeValue([]byte(invalid)); !errors.Is(err, cache.ErrInvalidValue) {
			t.Fatal(invalid, err)
		}
	}
}
