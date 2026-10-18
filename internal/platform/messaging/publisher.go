package messaging

import (
	"context"
	"errors"
	"fmt"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/wisalgroup/labena/pkg/database"
)

// Publisher publishes application payloads without exposing Watermill messages
// to callers. Publication rejects framework SQL transaction scopes and canceled
// contexts before serialization or transport effects. Native publishers bypass this
// policy; publication after commit is still best-effort. Publishers are borrowed;
// application composition owns transport cleanup.
type Publisher interface {
	Publish(
		ctx context.Context,
		topic string,
		payload any,
	) error

	PublishWithID(
		ctx context.Context,
		topic string,
		id string,
		payload any,
	) error
}

type publisher struct {
	publisher message.Publisher
}

// NewPublisher borrows a required native publisher without I/O or registration.
// The caller owns closing the transport after publication and handlers finish.
func NewPublisher(p message.Publisher) (Publisher, error) {
	if p == nil {
		return nil, errors.New("messaging: publisher is required")
	}
	return &publisher{publisher: p}, nil
}

func (p *publisher) Publish(
	ctx context.Context,
	topic string,
	payload any,
) error {
	if err := database.RequireNoTransaction(ctx); err != nil {
		return err
	}
	msg, err := NewJSONMessage(ctx, payload)
	if err != nil {
		return err
	}

	return p.publish(topic, msg)
}

func (p *publisher) PublishWithID(
	ctx context.Context,
	topic string,
	id string,
	payload any,
) error {
	if err := database.RequireNoTransaction(ctx); err != nil {
		return err
	}
	msg, err := NewJSONMessageWithID(ctx, id, payload)
	if err != nil {
		return err
	}

	return p.publish(topic, msg)
}

func (p *publisher) publish(
	topic string,
	msg *message.Message,
) error {
	if topic == "" {
		return ErrEmptyTopic
	}

	if err := p.publisher.Publish(topic, msg); err != nil {
		return fmt.Errorf(
			"messaging: publish to %q: %w",
			topic,
			err,
		)
	}

	return nil
}
