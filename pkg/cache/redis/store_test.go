package redis_test

import (
	"context"
	"crypto/rand"
	"fmt"
	native "github.com/redis/go-redis/v9"
	"github.com/wisalgroup/labena/pkg/cache"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	cacheredis "github.com/wisalgroup/labena/pkg/cache/redis"
	"github.com/wisalgroup/labena/pkg/redis"
)

func TestNativeExpiryNamespacesAndBackendErrors(t *testing.T) {
	url := os.Getenv("CACHE_TEST_REDIS")
	if url == "" {
		t.Skip("CACHE_TEST_REDIS not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := redis.OpenURL(ctx, url, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	namespace := "test_" + rand.Text()
	key := "cache:" + namespace + ":entry"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.Del(ctx, key)
	})
	store, err := cacheredis.New(client, cacheredis.Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	other, err := cacheredis.New(client, cacheredis.Config{Namespace: namespace + "_other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "entry", []byte("value"), time.Duration(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if ttl, err := client.Do(ctx, "PTTL", key).Int64(); err != nil || ttl < time.Hour.Milliseconds() {
		t.Fatal("TTL overflow or missing native expiry", ttl, err)
	}
	if _, found, err := other.Get(ctx, "entry"); err != nil || found {
		t.Fatal("namespace collision", found, err)
	}
	if err := store.Set(ctx, "entry", nil, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if ttl, err := client.PTTL(ctx, key).Result(); err != nil || ttl <= 0 || ttl > 2*time.Second {
		t.Fatal("replacement did not reset TTL", ttl, err)
	}
	if err := client.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, key, "field", "wrong type").Err(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(ctx, "entry"); err == nil || found {
		t.Fatal("backend type error treated as miss")
	}
	if err := store.Delete(ctx, "entry"); err != nil {
		t.Fatal(err)
	}
	if _, err := cacheredis.New(client, cacheredis.Config{Namespace: "bad:namespace"}); err == nil {
		t.Fatal("accepted ambiguous namespace")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(ctx, "entry"); err == nil || found {
		t.Fatal("closed backend treated as miss")
	}
}

func TestClearGlobNamespaceAndBulkErrors(t *testing.T) {
	url := os.Getenv("CACHE_TEST_REDIS")
	if url == "" {
		t.Skip("CACHE_TEST_REDIS not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := redis.OpenURL(ctx, url, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	namespace := "test_" + rand.Text()
	s, _ := cacheredis.New(client, cacheredis.Config{Namespace: namespace + "*[?]\\"})
	other, _ := cacheredis.New(client, cacheredis.Config{Namespace: namespace + "_other"})
	defer s.Clear(context.Background())
	defer other.Clear(context.Background())
	items := map[string]any{}
	for i := range 600 {
		items[fmt.Sprintf("key-%d", i)] = i
	}
	if err := cache.SetMulti[any](ctx, s, items, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := cache.SetMulti[any](ctx, other, map[string]any{"preserve": true}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	for key := range items {
		if exists, err := s.Exists(ctx, key); err != nil || exists {
			t.Fatal("clear missed", key, exists, err)
		}
	}
	if exists, err := other.Exists(ctx, "preserve"); err != nil || !exists {
		t.Fatal("glob leaked namespace", exists, err)
	}
	// A missing GET may be the pipeline's first error; later WRONGTYPE errors
	// must still propagate rather than being swallowed as misses.
	badKey := "cache:" + namespace + "*[?]\\:bad"
	if err := client.HSet(ctx, badKey, "field", "value").Err(); err != nil {
		t.Fatal(err)
	}
	if values, err := cache.GetMulti[any](ctx, s, []string{"missing", "bad"}); err == nil || values != nil {
		t.Fatal(values, err)
	}
	if err := cache.SetMulti[any](ctx, s, map[string]any{"expires": true}, time.Hour); err != nil {
		t.Fatal(err)
	}
	key := "cache:" + namespace + "*[?]\\:expires"
	if ttl, err := client.PTTL(ctx, key).Result(); err != nil || ttl <= 0 || ttl > time.Hour {
		t.Fatal(ttl, err)
	}
	if _, err := cache.Remember[any](ctx, s, "forever", cache.MaxTTL, func(context.Context) (any, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	if ttl, err := client.Do(ctx, "PTTL", "cache:"+namespace+"*[?]\\:forever").Int64(); err != nil || ttl < (290*365*24*time.Hour).Milliseconds() {
		t.Fatal(ttl, err)
	}
	// The adapter borrows the client: Clear must leave it usable.
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestClusterBulkAndClear(t *testing.T) {
	addresses := os.Getenv("CACHE_TEST_REDIS_CLUSTER")
	if addresses == "" {
		t.Skip("CACHE_TEST_REDIS_CLUSTER not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := redis.Open(ctx, native.UniversalOptions{Addrs: strings.Split(addresses, ",")}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	namespace := "cluster_" + rand.Text()
	s, _ := cacheredis.New(client, cacheredis.Config{Namespace: namespace})
	other, _ := cacheredis.New(client, cacheredis.Config{Namespace: namespace + "_other"})
	defer s.Clear(context.Background())
	defer other.Clear(context.Background())
	// Different hash tags force different hash slots; enough keys exercise all
	// three primaries in the disposable test cluster.
	items := map[string]any{}
	keys := []string{}
	for i := range 100 {
		key := fmt.Sprintf("{%d}:item", i)
		keys = append(keys, key)
		items[key] = fmt.Sprintf("value-%d", i)
	}
	if err := cache.SetMulti[any](ctx, s, items, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := cache.GetMulti[any](ctx, s, keys)
	if err != nil || !reflect.DeepEqual(got, items) {
		t.Fatal("cluster bulk", len(got), err)
	}
	if err := cache.SetMulti[any](ctx, other, map[string]any{"keep": true}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := cache.GetMulti[any](ctx, s, keys); err != nil || len(got) != 0 {
		t.Fatal("cluster clear", len(got), err)
	}
	if exists, err := other.Exists(ctx, "keep"); err != nil || !exists {
		t.Fatal("namespace isolation", exists, err)
	}
}
