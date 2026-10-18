package memory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/wisalgroup/labena/pkg/cache"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestBoundsExpiryAndOwnership(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		s, _ := New(1, 16)
		original := []byte("value")
		if err := s.Set(ctx, "a", original, time.Second); err != nil {
			t.Fatal(err)
		}
		original[0] = 'X'
		got, ok, err := s.Get(ctx, "a")
		if err != nil || !ok || string(got) != "value" {
			t.Fatal(string(got), ok, err)
		}
		got[0] = 'Y'
		got, _, _ = s.Get(ctx, "a")
		if string(got) != "value" {
			t.Fatal("mutable cache value")
		}
		if err := s.Set(ctx, "b", []byte("b"), time.Second); !errors.Is(err, cache.ErrFull) {
			t.Fatal("unbounded cache")
		}
		time.Sleep(time.Second)
		if err := s.Set(ctx, "b", []byte("b"), time.Second); err != nil {
			t.Fatal(err)
		}
		_, ok, _ = s.Get(ctx, "a")
		if ok {
			t.Fatal("expired hit")
		}
		_ = s.Close(ctx)
		_ = s.Close(ctx)
		if err := s.Set(ctx, "c", nil, time.Second); !errors.Is(err, cache.ErrClosed) {
			t.Fatal("write after close")
		}
	})
}

func TestByteBudget(t *testing.T) {
	ctx := context.Background()
	s, err := NewWithConfig(Config{Capacity: 10, MaxEntryBytes: 16, MaxBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	set := func(key, value string, want error) {
		t.Helper()
		if err := s.Set(ctx, key, []byte(value), time.Hour); !errors.Is(err, want) {
			t.Fatalf("Set(%q): %v, want %v", key, err, want)
		}
	}
	set("a", "123", nil)
	set("b", "123", nil)
	set("c", "", cache.ErrFull)
	set("a", "1234", cache.ErrFull)
	if got, ok, err := s.Get(ctx, "a"); err != nil || !ok || string(got) != "123" {
		t.Fatal("failed replacement changed value", string(got), ok, err)
	}
	set("a", "1", nil)
	set("c", "1", nil)
	if got := s.Usage(); got.Bytes != 8 || got.Entries != 3 {
		t.Fatal(got)
	}
	if err := s.Delete(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	set("c", "12345", nil)
	set("huge", "12345", cache.ErrTooLarge)
	if got := s.Usage(); got.Bytes != 8 || got.Entries != 2 {
		t.Fatal(got)
	}
	_ = s.Close(ctx)
	if got := s.Usage(); got.Bytes != 0 || got.Entries != 0 || !got.Closed {
		t.Fatal(got)
	}
}

func TestPruneAndRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		s, _ := NewWithConfig(Config{Capacity: 10, MaxEntryBytes: 16, MaxBytes: 12})
		for _, key := range []string{"a", "b", "c"} {
			if err := s.Set(ctx, key, []byte("123"), time.Second); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Set(ctx, "a", []byte("123"), time.Hour); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		if n, err := s.PruneExpired(ctx, 1); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		if got := s.Usage(); got.Entries != 2 || got.Bytes != 8 {
			t.Fatal(got)
		}
		if n, err := s.PruneExpired(ctx, 10); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		if _, ok, err := s.Get(ctx, "a"); err != nil || !ok {
			t.Fatal("refresh was pruned", ok, err)
		}
		// Expired bytes are reclaimed on admission even below the entry-count limit.
		if err := s.Set(ctx, "b", []byte("1234567"), time.Second); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		if err := s.Set(ctx, "c", []byte("1234567"), time.Hour); err != nil {
			t.Fatal(err)
		}
		if got := s.Usage(); got.Bytes != 12 || got.Entries != 2 {
			t.Fatal(got)
		}
		if err := s.Set(ctx, "d", nil, time.Hour); !errors.Is(err, cache.ErrFull) {
			t.Fatal(err)
		}
	})
}

func TestGetIntoOwnership(t *testing.T) {
	ctx := context.Background()
	s, _ := New(2, 16)
	if err := s.Set(ctx, "a", []byte("value"), time.Hour); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 0, 16)
	got, ok, err := s.GetInto(ctx, "a", append(buf, '!'))
	if err != nil || !ok || string(got) != "!value" {
		t.Fatal(string(got), ok, err)
	}
	got[1] = 'X'
	got, ok, err = s.GetInto(ctx, "a", got[:0])
	if err != nil || !ok || string(got) != "value" {
		t.Fatal(string(got), ok, err)
	}
	got, ok, err = s.GetInto(ctx, "missing", got)
	if err != nil || ok || string(got) != "value" {
		t.Fatal(string(got), ok, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	got, ok, err = s.GetInto(canceled, "a", got)
	if !errors.Is(err, context.Canceled) || ok || string(got) != "value" {
		t.Fatal(string(got), ok, err)
	}
}

func TestLimitsAndClosedOperations(t *testing.T) {
	for _, cfg := range []Config{{}, {Capacity: -1, MaxEntryBytes: 1}, {Capacity: 1, MaxEntryBytes: 0}, {Capacity: 1, MaxEntryBytes: 1, MaxBytes: -1}} {
		if _, err := NewWithConfig(cfg); err == nil {
			t.Fatal("accepted", cfg)
		}
	}
	ctx := context.Background()
	s, _ := New(1, 16)
	if s.Usage().MaxBytes != DefaultMaxBytes {
		t.Fatal(s.Usage())
	}
	for _, limit := range []int{0, -1, 10001} {
		if _, err := s.PruneExpired(ctx, limit); err == nil {
			t.Fatal("accepted limit", limit)
		}
	}
	_ = s.Close(ctx)
	if _, _, err := s.Get(ctx, "a"); !errors.Is(err, cache.ErrClosed) {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "a"); !errors.Is(err, cache.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := s.PruneExpired(ctx, 1); !errors.Is(err, cache.ErrClosed) {
		t.Fatal(err)
	}
}

func TestConcurrentOperationsAndClose(t *testing.T) {
	ctx := context.Background()
	s, _ := NewWithConfig(Config{Capacity: 8, MaxEntryBytes: 256, MaxBytes: 1024})
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			<-start
			key := fmt.Sprintf("key-%d", i)
			value := bytes.Repeat([]byte{byte(i)}, 128)
			for range 100 {
				if err := s.Set(ctx, key, value, time.Hour); err != nil && !errors.Is(err, cache.ErrClosed) && !errors.Is(err, cache.ErrFull) {
					t.Error(err)
				}
				got, ok, err := s.Get(ctx, key)
				if err != nil && !errors.Is(err, cache.ErrClosed) {
					t.Error(err)
				}
				if ok && !bytes.Equal(got, value) {
					t.Error("torn value")
				}
				if _, err := s.PruneExpired(ctx, 1); err != nil && !errors.Is(err, cache.ErrClosed) {
					t.Error(err)
				}
				if err := s.Delete(ctx, key); err != nil && !errors.Is(err, cache.ErrClosed) {
					t.Error(err)
				}
				if usage := s.Usage(); usage.Bytes < 0 || usage.Bytes > usage.MaxBytes || usage.Entries > usage.Capacity {
					t.Error(usage)
				}
			}
		})
	}
	wg.Go(func() { <-start; _ = s.Close(ctx) })
	close(start)
	wg.Wait()
	if got := s.Usage(); got.Entries != 0 || got.Bytes != 0 || !got.Closed {
		t.Fatal(got)
	}
}

func TestCancellationWhileWaitingForLock(t *testing.T) {
	s, _ := New(1, 16)
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- s.Set(ctx, "a", []byte("v"), time.Hour) }()
	cancel()
	s.mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled writer did not finish")
	}
	if got := s.Usage(); got.Entries != 0 {
		t.Fatal(got)
	}
}

func TestConcurrentAdmissionHonorsByteBudget(t *testing.T) {
	ctx := context.Background()
	s, _ := NewWithConfig(Config{Capacity: 100, MaxEntryBytes: 16, MaxBytes: 32})
	start := make(chan struct{})
	var wg sync.WaitGroup
	var admitted atomic.Int64
	for i := range 32 {
		wg.Go(func() {
			<-start
			key := fmt.Sprintf("%02d", i)
			err := s.Set(ctx, key, []byte("123456"), time.Hour)
			if err == nil {
				admitted.Add(1)
			} else if !errors.Is(err, cache.ErrFull) {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	if admitted.Load() != 4 {
		t.Fatal("admitted", admitted.Load())
	}
	if got := s.Usage(); got.Entries != 4 || got.Bytes != 32 {
		t.Fatal(got)
	}
}
