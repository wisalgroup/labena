package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
)

func TestLocalBusAdmissionAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bus := NewLocalBus(nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		_, err := bus.Subscribe(ctx, "blocked")
		if err != nil {
			t.Fatal(err)
		}
		messages := make([]*message.Message, MaxPendingEvents)
		for i := range messages {
			messages[i] = message.NewMessage(fmt.Sprint(i), nil)
		}
		if err = bus.Publish("blocked", messages...); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err = bus.Publish("blocked", message.NewMessage("overflow", nil)); !errors.Is(err, ErrEventCapacity) {
			t.Fatal(err)
		}
		if err = bus.Publish("blocked", message.NewMessage("large", make([]byte, MaxEventBytes))); !errors.Is(err, ErrEventTooLarge) {
			t.Fatal(err)
		}
		if err = bus.Close(); err != nil {
			t.Fatal(err)
		}
		if err = bus.Publish("blocked", message.NewMessage("closed", nil)); !errors.Is(err, ErrLocalBusClosed) {
			t.Fatal(err)
		}
	})
}

func TestLocalBusBoundsHandlersAndSettlesFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bus := NewLocalBus(nil)
		defer bus.Close()
		router, _ := message.NewRouter(message.RouterConfig{}, watermill.NopLogger{})
		runner, err := RegisterRunner(router)
		if err != nil {
			t.Fatal(err)
		}
		router.AddMiddleware(bus.DeliveryMiddleware)
		retry := middleware.Retry{MaxRetries: 3, InitialInterval: time.Millisecond, Multiplier: 2}
		router.AddMiddleware(retry.Middleware)
		var active, peak, calls atomic.Int32
		release := make(chan struct{})
		for i := range 32 {
			topic := fmt.Sprint(i)
			router.AddConsumerHandler(topic, topic, bus, func(*message.Message) error {
				n := active.Add(1)
				for old := peak.Load(); n > old; old = peak.Load() {
					if peak.CompareAndSwap(old, n) {
						break
					}
				}
				defer active.Add(-1)
				calls.Add(1)
				<-release
				return errors.New("permanent failure")
			})
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- runner.Run(ctx) }()
		<-runner.Ready()
		for i := range 32 {
			if err = bus.Publish(fmt.Sprint(i), message.NewMessage(fmt.Sprint(i), nil)); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if peak.Load() != LocalHandlerConcurrency {
			t.Fatal("concurrency", peak.Load())
		}
		close(release)
		time.Sleep(time.Second)
		synctest.Wait()
		if calls.Load() != 32*4 {
			t.Fatal("unexpected retries or redelivery", calls.Load())
		}
		if peak.Load() > LocalHandlerConcurrency {
			t.Fatal("concurrency exceeded", peak.Load())
		}
		// Capacity has been released after failed deliveries were settled.
		messages := make([]*message.Message, MaxPendingEvents)
		for i := range messages {
			messages[i] = message.NewMessage(fmt.Sprint(i), nil)
		}
		if err = bus.Publish("no-subscriber", messages...); err != nil {
			t.Fatal(err)
		}
		cancel()
		if err = <-done; err != nil {
			t.Fatal(err)
		}
	})
}
