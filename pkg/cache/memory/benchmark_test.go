package memory

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/cache"
)

// Fixed payloads and precomputed keys keep encoding and key generation out of
// timed operations. These closed-loop benchmarks do not measure service p99.
func BenchmarkStore(b *testing.B) {
	for _, count := range []int{100, 10000} {
		b.Run(fmt.Sprintf("entries=%d", count), func(b *testing.B) {
			s, err := New(count, 2048)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = s.Close(context.Background()) })
			ctx := context.Background()
			value := make([]byte, 1024)
			keys := make([]string, count)
			for i := range keys {
				keys[i] = fmt.Sprintf("key-%d", i)
				if err := s.Set(ctx, keys[i], value, time.Hour); err != nil {
					b.Fatal(err)
				}
			}
			b.Run("Get", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, found, err := s.Get(ctx, keys[i%count]); err != nil || !found {
						b.Fatal(found, err)
					}
				}
			})
			b.Run("GetParallel", func(b *testing.B) {
				b.ReportAllocs()
				b.RunParallel(func(pb *testing.PB) {
					i := 0
					for pb.Next() {
						if _, found, err := s.Get(ctx, keys[i%count]); err != nil || !found {
							b.Error(found, err)
							return
						}
						i++
					}
				})
			})
			b.Run("GetInto", func(b *testing.B) {
				buf := make([]byte, 0, len(value))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					got, found, err := s.GetInto(ctx, keys[i%count], buf[:0])
					if err != nil || !found {
						b.Fatal(found, err)
					}
					buf = got
				}
			})
			b.Run("Update", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if err := s.Set(ctx, keys[i%count], value, time.Hour); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("RejectFull", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if err := s.Set(ctx, "new-key", value, time.Hour); !errors.Is(err, cache.ErrFull) {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
