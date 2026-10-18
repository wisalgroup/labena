package testdb_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/wisalgroup/labena/pkg/cache"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Called by the shared contract against memory and each real target backend.
func checkValueOperations(t *testing.T, store cache.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := store.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	checkRawBatches(t, ctx, store)
	checkTypedRemember(t, ctx, store)
	items := map[string]any{"object": map[string]any{"id": json.Number("9007199254740993")}, "null": nil, "text": "hello", "array": []any{true, "x"}}
	if err := cache.SetMulti[any](ctx, store, items, time.Minute); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(ctx, "null"); err != nil || !exists {
		t.Fatal("null must exist", exists, err)
	}
	if exists, err := store.Exists(ctx, "missing"); err != nil || exists {
		t.Fatal(exists, err)
	}
	got, err := cache.GetMulti[any](ctx, store, []string{"object", "null", "text", "array", "missing", "text"})
	if err != nil || !reflect.DeepEqual(got, items) {
		t.Fatalf("GetMulti: %#v, %v", got, err)
	}
	got["object"].(map[string]any)["id"] = "changed"
	again, err := cache.GetMulti[any](ctx, store, []string{"object"})
	if err != nil || !reflect.DeepEqual(again["object"], items["object"]) {
		t.Fatal("aliased value", again, err)
	}
	if got, err := cache.GetMulti[any](ctx, store, nil); err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if err := cache.SetMulti[any](ctx, store, nil, time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{
		{"text": "changed", "bad\x00key": "invalid"},
		{"text": "changed", "unsupported": func() {}},
		{"text": "changed", "large": strings.Repeat("x", 257)},
	} {
		if err := cache.SetMulti[any](ctx, store, bad, time.Minute); err == nil {
			t.Fatal("invalid batch accepted")
		}
		values, err := cache.GetMulti[any](ctx, store, []string{"text"})
		if err != nil || values["text"] != "hello" {
			t.Fatal("validation partially wrote", values, err)
		}
	}
	if _, err := cache.GetMulti[any](ctx, store, []string{"text", ""}); !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatal(err)
	}
	if _, err := store.Exists(ctx, ""); !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatal(err)
	}
	if err := cache.SetMulti[any](ctx, store, items, 0); !errors.Is(err, cache.ErrInvalidTTL) {
		t.Fatal(err)
	}
	tooMany := make([]string, cache.MaxBatchKeys+1)
	if _, err := cache.GetMulti[any](ctx, store, tooMany); !errors.Is(err, cache.ErrTooLarge) {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "raw", []byte("not-json"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(ctx, "raw"); err != nil || !exists {
		t.Fatal(exists, err)
	}
	if values, err := cache.GetMulti[any](ctx, store, []string{"text", "raw"}); !errors.Is(err, cache.ErrInvalidValue) || values != nil {
		t.Fatal(values, err)
	}
	calls := 0
	loader := func(context.Context) (any, error) { calls++; return map[string]any{"value": 42}, nil }
	first, err := store.Remember(ctx, "loaded", time.Minute, loader)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Remember(ctx, "loaded", time.Minute, loader)
	if err != nil || calls != 1 || !reflect.DeepEqual(first, second) {
		t.Fatal(first, second, calls, err)
	}
	nullCalls := 0
	nullLoader := func(context.Context) (any, error) { nullCalls++; return nil, nil }
	for range 2 {
		if value, err := store.Remember(ctx, "forever", cache.MaxTTL, nullLoader); err != nil || value != nil {
			t.Fatal(value, err)
		}
	}
	if nullCalls != 1 {
		t.Fatal("null reloaded", nullCalls)
	}
	sentinel := errors.New("load failed")
	if _, err := store.Remember(ctx, "failed", time.Minute, func(context.Context) (any, error) { return nil, sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if exists, err := store.Exists(ctx, "failed"); err != nil || exists {
		t.Fatal(exists, err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := store.Clear(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := store.Exists(canceled, "text"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := cache.GetMulti[any](canceled, store, []string{"text"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := cache.SetMulti[any](canceled, store, map[string]any{"text": "bad"}, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := store.Remember(canceled, "cancel", time.Minute, loader); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("canceled load executed")
	}
	if err := store.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"text", "raw", "loaded", "forever"} {
		if exists, err := store.Exists(ctx, key); err != nil || exists {
			t.Fatal("clear missed", key, exists, err)
		}
	}
	if err := store.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cache.SetMulti[any](ctx, store, map[string]any{"reused": true}, time.Minute); err != nil {
		t.Fatal("clear closed store", err)
	}
}

// Typed cache results must preserve precision and caller ownership on both paths.
func checkTypedRemember(t *testing.T, ctx context.Context, store cache.Store) {
	t.Helper()
	type product struct {
		ID     int64    `json:"id"`
		Labels []string `json:"labels"`
	}
	calls := 0
	original := product{ID: 9007199254740993, Labels: []string{"original"}}
	load := func(context.Context) (product, error) { calls++; return original, nil }
	first, err := cache.Remember(ctx, store, "typed:v1:product", time.Minute, load)
	if err != nil || first.ID != original.ID {
		t.Fatalf("typed miss: %+v %v", first, err)
	}
	first.Labels[0] = "changed"
	if original.Labels[0] != "original" {
		t.Fatal("miss result aliases loader data")
	}
	second, err := cache.Remember(ctx, store, "typed:v1:product", time.Minute, load)
	if err != nil || second.ID != original.ID || second.Labels[0] != "original" || calls != 1 {
		t.Fatalf("typed hit: %+v %v; calls=%d", second, err, calls)
	}
	if err := store.Set(ctx, "typed:v1:wrong", []byte(`"wrong type"`), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Remember(ctx, store, "typed:v1:wrong", time.Minute, load); !errors.Is(err, cache.ErrInvalidValue) {
		t.Fatalf("wrong cached type: %v", err)
	}
	if calls != 1 {
		t.Fatal("type mismatch was treated as a miss")
	}
}

// The byte contract must work for non-JSON values and agree with single-key I/O.
func checkRawBatches(t *testing.T, ctx context.Context, store cache.Store) {
	t.Helper()
	original := []byte{0, 255, 128, 1}
	want := append([]byte(nil), original...)
	if err := store.SetMulti(ctx, map[string][]byte{"binary": original, "empty": nil}, time.Minute); err != nil {
		t.Fatal(err)
	}
	original[0] = 42
	values, err := store.GetMulti(ctx, []string{"binary", "binary", "empty", "absent"})
	if err != nil || len(values) != 2 || !bytes.Equal(values["binary"], want) {
		t.Fatalf("raw batch: %v %v", values, err)
	}
	if value, ok := values["empty"]; !ok || len(value) != 0 {
		t.Fatal("empty entry became a miss")
	}
	values["binary"][0] = 99
	single, found, err := store.Get(ctx, "binary")
	if err != nil || !found || !bytes.Equal(single, want) {
		t.Fatal("batch result aliases stored bytes", single, err)
	}
	if err := store.Set(ctx, "single", []byte("plain text"), time.Minute); err != nil {
		t.Fatal(err)
	}
	values, err = store.GetMulti(ctx, []string{"single"})
	if err != nil || string(values["single"]) != "plain text" {
		t.Fatal("single/bulk encoding mismatch", values, err)
	}
	if result, err := cache.GetMulti[any](ctx, store, []string{"binary"}); !errors.Is(err, cache.ErrInvalidValue) || result != nil {
		t.Fatal("JSON helper accepted binary", result, err)
	}
	if err := store.SetMulti(ctx, map[string][]byte{"binary": []byte("changed"), "": []byte("invalid")}, time.Minute); !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatal(err)
	}
	single, _, err = store.Get(ctx, "binary")
	if err != nil || !bytes.Equal(single, want) {
		t.Fatal("invalid batch partially wrote", err)
	}
	if err := store.SetMulti(ctx, map[string][]byte{"binary": []byte("changed"), "oversized": make([]byte, 257)}, time.Minute); !errors.Is(err, cache.ErrTooLarge) {
		t.Fatal("raw entry bound", err)
	}
	single, _, err = store.Get(ctx, "binary")
	if err != nil || !bytes.Equal(single, want) {
		t.Fatal("oversized raw batch partially wrote", err)
	}
	if _, err := store.GetMulti(ctx, make([]string, cache.MaxBatchKeys+1)); !errors.Is(err, cache.ErrTooLarge) {
		t.Fatal("raw key-count bound", err)
	}
	type item struct {
		ID int64 `json:"id"`
	}
	typed := map[string]item{"typed:batch": {ID: 9007199254740993}}
	if err := cache.SetMulti(ctx, store, typed, time.Minute); err != nil {
		t.Fatal(err)
	}
	loaded, err := cache.GetMulti[item](ctx, store, []string{"typed:batch", "absent"})
	if err != nil || !reflect.DeepEqual(loaded, typed) {
		t.Fatal("typed bulk round trip", loaded, err)
	}
	empty, err := store.GetMulti(ctx, nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("empty raw result", empty, err)
	}
	if err := store.SetMulti(ctx, nil, time.Minute); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.GetMulti(canceled, []string{"binary"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.SetMulti(canceled, map[string][]byte{"binary": nil}, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
