package testdb_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/cache"
	cachedb "github.com/wisalgroup/labena/pkg/cache/database"
	"github.com/wisalgroup/labena/pkg/cache/memory"
	cacheredis "github.com/wisalgroup/labena/pkg/cache/redis"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/redis"
)

func TestStoreContract(t *testing.T) {
	for _, kind := range []string{"memory", "postgres", "mysql", "redis"} {
		t.Run(kind, func(t *testing.T) {
			var store cache.Store
			switch kind {
			case "memory":
				s, err := memory.New(100, 256)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { s.Close(context.Background()) })
				store = s
			case "postgres", "mysql":
				db := testdb.OpenDriver(t, database.Driver(kind))
				s, err := cachedb.New(db, cachedb.Config{MaxEntryBytes: 256})
				if err != nil {
					t.Fatal(err)
				}
				store = s
			case "redis":
				url := os.Getenv("CACHE_TEST_REDIS")
				if url == "" {
					t.Skip("CACHE_TEST_REDIS not set")
				}
				client, err := redis.OpenURL(context.Background(), url, 5*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { client.Close() })
				namespace := "test_" + rand.Text()
				s, err := cacheredis.New(client, cacheredis.Config{Namespace: namespace, MaxEntryBytes: 256})
				if err != nil {
					t.Fatal(err)
				}
				store = s
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					var cursor uint64
					for {
						keys, next, err := client.Scan(ctx, cursor, "cache:"+namespace+":*", 100).Result()
						if err != nil {
							t.Error(err)
							return
						}
						if len(keys) > 0 {
							if err := client.Del(ctx, keys...).Err(); err != nil {
								t.Error(err)
							}
						}
						cursor = next
						if cursor == 0 {
							break
						}
					}
				})
			}
			checkContract(t, store)
			checkValueOperations(t, store)
		})
	}
}

func checkContract(t *testing.T, store cache.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	get := func(key string, want []byte, found bool) {
		t.Helper()
		got, ok, err := store.Get(ctx, key)
		if err != nil || ok != found || !bytes.Equal(got, want) {
			t.Fatalf("Get(%q) = %x, %v, %v; want %x, %v", key, got, ok, err, want, found)
		}
	}
	set := func(key string, value []byte, ttl time.Duration) {
		t.Helper()
		if err := store.Set(ctx, key, value, ttl); err != nil {
			t.Fatal(err)
		}
	}
	get("missing", nil, false)
	value := []byte{0, 255, 'a', 0}
	set("binary", value, time.Minute)
	value[0] = 42
	get("binary", []byte{0, 255, 'a', 0}, true)
	got, _, err := store.Get(ctx, "binary")
	if err != nil {
		t.Fatal(err)
	}
	got[0] = 64
	get("binary", []byte{0, 255, 'a', 0}, true)
	set("empty", nil, time.Minute)
	get("empty", nil, true)
	for _, key := range []string{"case", "Case", "case ", "quote'\\☃", strings.Repeat("k", 255)} {
		set(key, []byte{}, time.Minute)
		get(key, nil, true)
	}
	for _, key := range []string{"", strings.Repeat("k", 256), "bad\x00key", string([]byte{255})} {
		if _, _, err := store.Get(ctx, key); !errors.Is(err, cache.ErrInvalidKey) {
			t.Fatal("invalid get key", err)
		}
		if err := store.Set(ctx, key, nil, time.Second); !errors.Is(err, cache.ErrInvalidKey) {
			t.Fatal("invalid set key", err)
		}
		if err := store.Delete(ctx, key); !errors.Is(err, cache.ErrInvalidKey) {
			t.Fatal("invalid delete key", err)
		}
	}
	for _, ttl := range []time.Duration{0, -time.Second} {
		if err := store.Set(ctx, "binary", nil, ttl); !errors.Is(err, cache.ErrInvalidTTL) {
			t.Fatal(err)
		}
	}
	if err := store.Set(ctx, "binary", make([]byte, 256), time.Minute); !errors.Is(err, cache.ErrTooLarge) {
		t.Fatal(err)
	}
	get("binary", []byte{0, 255, 'a', 0}, true)
	if err := store.Delete(ctx, "binary"); err != nil {
		t.Fatal(err)
	}
	get("binary", nil, false)
	if err := store.Delete(ctx, "binary"); err != nil {
		t.Fatal(err)
	}
	set("renew", []byte("old"), time.Millisecond)
	set("renew", []byte("new"), time.Minute)
	set("expires", nil, 10*time.Millisecond)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		_, found, err := store.Get(ctx, "expires")
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("TTL did not expire")
		case <-ticker.C:
		}
	}
	get("renew", []byte("new"), true)
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := store.Set(canceled, "renew", nil, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err := store.Get(canceled, "renew"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.Delete(canceled, "renew"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	get("renew", []byte("new"), true)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			value := bytes.Repeat([]byte{byte(i)}, 64)
			for range 8 {
				if err := store.Set(ctx, "shared", value, time.Minute); err != nil {
					t.Error(err)
					return
				}
				got, ok, err := store.Get(ctx, "shared")
				if err != nil || !ok || len(got) != 64 || !bytes.Equal(got, bytes.Repeat(got[:1], 64)) {
					t.Errorf("non-atomic value: %x %v %v", got, ok, err)
					return
				}
			}
		})
	}
	wg.Wait()
}
