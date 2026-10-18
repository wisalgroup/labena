package redis_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	native "github.com/redis/go-redis/v9"
	"github.com/wisalgroup/labena/pkg/cache"
	cacheredis "github.com/wisalgroup/labena/pkg/cache/redis"
	"github.com/wisalgroup/labena/pkg/redis"
)

type replyMeter struct{ bytes int }

func (*replyMeter) DialHook(next native.DialHook) native.DialHook { return next }
func (m *replyMeter) ProcessHook(next native.ProcessHook) native.ProcessHook {
	return func(ctx context.Context, cmd native.Cmder) error {
		err := next(ctx, cmd)
		m.record(cmd)
		return err
	}
}
func (m *replyMeter) ProcessPipelineHook(next native.ProcessPipelineHook) native.ProcessPipelineHook {
	return func(ctx context.Context, cmds []native.Cmder) error {
		err := next(ctx, cmds)
		for _, cmd := range cmds {
			m.record(cmd)
		}
		return err
	}
}
func (m *replyMeter) record(cmd native.Cmder) {
	if v, ok := cmd.(*native.Cmd); ok {
		if s, err := v.Text(); err == nil {
			m.bytes += len(s)
		}
	}
	if v, ok := cmd.(*native.StringCmd); ok {
		if s, err := v.Result(); err == nil {
			m.bytes += len(s)
		}
	}
}

func TestBoundedRedisReads(t *testing.T) {
	url := os.Getenv("CACHE_TEST_REDIS")
	if url == "" {
		t.Skip("CACHE_TEST_REDIS not set")
	}
	client, err := redis.OpenURL(t.Context(), url, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	namespace := "bounds_" + rand.Text()
	store, err := cacheredis.New(client, cacheredis.Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Clear(context.Background())
	keys := make([]string, 64)
	value := make([]byte, (1<<20)-256)
	for i := range keys {
		keys[i] = fmt.Sprint(i)
		if err = store.Set(t.Context(), keys[i], value, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	meter := new(replyMeter)
	client.AddHook(meter)
	if result, err := store.GetMulti(t.Context(), keys); !errors.Is(err, cache.ErrTooLarge) || result != nil {
		t.Fatal(err)
	}
	if meter.bytes > 2*cache.MaxBatchBytes {
		t.Fatalf("fetched %d bytes before rejection", meter.bytes)
	}
	// A legacy or external writer cannot force an oversized reply, even in Get.
	if err = client.Set(t.Context(), "cache:"+namespace+":legacy", make([]byte, 2<<20), time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	meter.bytes = 0
	if _, _, err = store.Get(t.Context(), "legacy"); !errors.Is(err, cache.ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err = store.GetMulti(t.Context(), []string{"absent", "legacy"}); !errors.Is(err, cache.ErrTooLarge) {
		t.Fatal(err)
	}
	if meter.bytes != 0 {
		t.Fatalf("oversized value transferred: %d", meter.bytes)
	}
}
