package identity_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/shared/events"
)

func TestUserRegisteredPayloadContainsOnlyOccurrenceAndUser(t *testing.T) {
	id := uuid.NewString()
	event := events.NewUserRegistered(uuid.MustParse(id))
	if event.ID == uuid.Nil || event.Type != events.UserRegisteredType || event.OccurredAt.IsZero() {
		t.Fatal("missing event metadata", event)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 {
		t.Fatalf("unexpected event fields: %s", payload)
	}
	for _, key := range []string{"event_id", "event_type", "occurred_at", "user_id"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("missing %s: %s", key, payload)
		}
	}
	var decoded events.UserRegistered
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.UserID.String() != id || decoded.ID != event.ID || !decoded.OccurredAt.Equal(event.OccurredAt) {
		t.Fatal("event metadata lost in transit")
	}
}
