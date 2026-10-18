// Package events defines published business events and their occurrence metadata.
// Publishing modules own each contract's meaning and version; consuming modules
// own their handlers. This package contains no publication or delivery behavior.
package events

import (
	"time"

	"github.com/google/uuid"
)

// Event contains metadata shared by all events.
type Event struct {
	ID         uuid.UUID `json:"event_id"`
	Type       string    `json:"event_type"`
	OccurredAt time.Time `json:"occurred_at"`
}

// NewEvent creates occurrence metadata with a fresh ID and UTC timestamp.
func NewEvent(eventType string) Event {
	return Event{
		ID:         uuid.Must(uuid.NewV7()),
		Type:       eventType,
		OccurredAt: time.Now().UTC(),
	}
}
