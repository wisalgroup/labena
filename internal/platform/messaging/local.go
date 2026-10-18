package messaging

import (
	"bytes"
	"context"
	"errors"
	"sync"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

// Local admission budgets apply per bus; handler concurrency spans all its topics.
const (
	MaxPendingEvents        = 128
	MaxEventBytes           = 64 << 10
	MaxPendingEventBytes    = 8 << 20
	LocalHandlerConcurrency = 16
)

var (
	ErrEventCapacity  = errors.New("messaging: local event capacity exhausted")
	ErrEventTooLarge  = errors.New("messaging: local event exceeds size limit")
	ErrLocalBusClosed = errors.New("messaging: local bus closed")
)

// LocalBus bounds asynchronous GoChannel publication. Successful Publish means
// in-memory admission only. Capacity is retained until every subscriber settles
// its delivery. Install DeliveryMiddleware outside retries on the native router:
// one exhausted retry cycle is acknowledged and discarded, never redelivered.
// Register subscribers before publication; producers must stop before Close.
type LocalBus struct {
	bus                   *gochannel.GoChannel
	logger                watermill.LoggerAdapter
	mu                    sync.Mutex
	pending, pendingBytes int
	closed                bool
	publications          sync.WaitGroup
	handlers              chan struct{}
}

// NewLocalBus constructs an ephemeral transport without starting background work.
func NewLocalBus(logger watermill.LoggerAdapter) *LocalBus {
	if logger == nil {
		logger = watermill.NopLogger{}
	}
	return &LocalBus{
		bus:    gochannel.NewGoChannel(gochannel.Config{OutputChannelBuffer: 64, BlockPublishUntilSubscriberAck: true}, logger),
		logger: logger, handlers: make(chan struct{}, LocalHandlerConcurrency),
	}
}

func (b *LocalBus) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	return b.bus.Subscribe(ctx, topic)
}

// Publish rejects an entire oversized/full batch before copying or starting work.
// Byte budgets include payload, metadata, ID and topic, not Go object overhead.
func (b *LocalBus) Publish(topic string, messages ...*message.Message) error {
	if topic == "" {
		return ErrEmptyTopic
	}
	if len(messages) == 0 {
		return nil
	}
	if len(messages) > MaxPendingEvents {
		return b.reject(ErrEventCapacity)
	}
	total := 0
	for _, msg := range messages {
		if msg == nil {
			return ErrEmptyMessageID
		}
		if err := msg.Context().Err(); err != nil {
			return err
		}
		size := len(msg.Payload) + len(msg.UUID) + len(topic)
		for key, value := range msg.Metadata {
			size += len(key) + len(value)
		}
		if size > MaxEventBytes {
			return b.reject(ErrEventTooLarge)
		}
		total += size
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrLocalBusClosed
	}
	if len(messages) > MaxPendingEvents-b.pending || total > MaxPendingEventBytes-b.pendingBytes {
		b.mu.Unlock()
		return b.reject(ErrEventCapacity)
	}
	b.pending += len(messages)
	b.pendingBytes += total
	b.publications.Add(1)
	// Copy while admission is reserved. Caller mutation after Publish is safe.
	copies := make([]*message.Message, len(messages))
	for i, msg := range messages {
		copies[i] = msg.Copy()
		copies[i].Payload = bytes.Clone(msg.Payload)
	}
	b.mu.Unlock()
	go func() {
		defer b.publications.Done()
		defer func() {
			b.mu.Lock()
			b.pending -= len(copies)
			b.pendingBytes -= total
			b.mu.Unlock()
		}()
		if err := b.bus.Publish(topic, copies...); err != nil {
			b.logger.Error("local event publication failed", err, nil)
		}
	}()
	return nil
}

func (b *LocalBus) reject(err error) error {
	b.logger.Error("local event rejected", err, nil)
	return err
}

// DeliveryMiddleware limits active retry cycles across all topics. Returning nil
// after a failed cycle settles this best-effort delivery; LogFailures inside this
// middleware records the final failure. Cancellation also settles waiting calls.
func (b *LocalBus) DeliveryMiddleware(next message.HandlerFunc) message.HandlerFunc {
	return func(msg *message.Message) ([]*message.Message, error) {
		select {
		case b.handlers <- struct{}{}:
			defer func() { <-b.handlers }()
		case <-msg.Context().Done():
			return nil, nil
		}
		result, err := next(msg)
		if err != nil {
			return nil, nil
		}
		return result, nil
	}
}

// Close releases blocked publication and joins every admitted publisher goroutine.
func (b *LocalBus) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	err := b.bus.Close()
	b.publications.Wait()
	return err
}
