package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
)

var (
	ErrEmptyTopic     = errors.New("messaging: topic is required")
	ErrEmptyMessageID = errors.New("messaging: message ID is required")
)

// NewJSONMessage creates a Watermill message containing a JSON-encoded payload.
//
// A new message ID is generated automatically.
func NewJSONMessage(
	ctx context.Context,
	payload any,
) (*message.Message, error) {
	return NewJSONMessageWithID(ctx, watermill.NewUUID(), payload)
}

// NewJSONMessageWithID creates a Watermill message containing a JSON-encoded
// payload using the supplied message ID.
//
// Prefer this when the application already has a stable event ID so that the
// event ID and transport message ID can be the same.
func NewJSONMessageWithID(
	ctx context.Context,
	id string,
	payload any,
) (*message.Message, error) {
	if id == "" {
		return nil, ErrEmptyMessageID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("messaging: marshal payload: %w", err)
	}

	return message.NewMessageWithContext(ctx, id, body), nil
}
