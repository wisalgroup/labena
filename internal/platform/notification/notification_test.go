package notification

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/pkg/mailview"
)

func TestMessageChannels(t *testing.T) {
	for _, tc := range []struct {
		message Message
		want    []Channel
	}{
		{Message{}, nil},
		{Message{Database: json.RawMessage(`{}`)}, []Channel{ChannelDatabase}},
		{Message{Email: &mailview.Email{}}, []Channel{ChannelEmail}},
		{Message{Database: json.RawMessage(`{}`), Email: &mailview.Email{}}, []Channel{ChannelDatabase, ChannelEmail}},
	} {
		if !slices.Equal(tc.message.Channels(), tc.want) {
			t.Fatal(tc.message.Channels(), tc.want)
		}
	}
	if Channel("").Valid() || Channel("unknown").Valid() || !ChannelDatabase.Valid() || !ChannelEmail.Valid() {
		t.Fatal("unsupported channel accepted")
	}
	if DeliveryStatus("").Valid() || DeliveryStatus("unknown").Valid() {
		t.Fatal("invalid delivery status accepted")
	}
}

func TestMessageValidationBeforeDelivery(t *testing.T) {
	notifier, err := New(nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	recipient := Recipient{Type: "user", ID: "42"}
	for _, tc := range []struct {
		name    string
		message Message
		want    error
	}{
		{"no channels", Message{Key: "1", Type: "welcome"}, ErrInvalidMessage},
		{"blank key", Message{Key: " ", Type: "welcome", Database: json.RawMessage(`{}`)}, ErrInvalidKey},
		{"ambiguous key", Message{Key: " 1", Type: "welcome", Database: json.RawMessage(`{}`)}, ErrInvalidKey},
		{"empty type", Message{Key: "1", Database: json.RawMessage(`{}`)}, ErrInvalidType},
		{"control type", Message{Key: "1", Type: "a\nb", Database: json.RawMessage(`{}`)}, ErrInvalidType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := notifier.Send(t.Context(), recipient, tc.message); !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
	for _, data := range []string{"", "null", "[]", `"text"`, "42", "true", "{", `{"a":1} {}`, string([]byte{'{', '"', 0xff, '"', ':', '1', '}'}), `{"body":"` + strings.Repeat("a", MaxDataSize) + `"}`} {
		message := Message{Key: "1", Type: "welcome", Database: json.RawMessage(data)}
		if _, err := notifier.Send(t.Context(), recipient, message); !errors.Is(err, ErrInvalidData) {
			t.Fatalf("data length %d: %v", len(data), err)
		}
		if _, err := notifier.Enqueue(t.Context(), recipient, message); !errors.Is(err, ErrInvalidData) {
			t.Fatalf("enqueue data length %d: %v", len(data), err)
		}
	}
}

func TestRecordOwnsJSONData(t *testing.T) {
	data := json.RawMessage(`{"title":"Hello","nested":{"count":9007199254740993},"items":[1,true,null]}`)
	record, err := newRecord(Recipient{Type: "admin", ID: "42", Email: "admin@example.com"}, "welcome", "welcome", data)
	if err != nil || record.validate() != nil || record.ID == uuid.Nil || record.Recipient.Email != "" {
		t.Fatal(record, err)
	}
	before := bytes.Clone(record.Data)
	data[2] = 'X'
	if !bytes.Equal(record.Data, before) {
		t.Fatal("record retained caller's mutable payload")
	}
}

func TestRecipientValidationBeforeEffects(t *testing.T) {
	notifier, err := New(nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, recipient := range []Recipient{
		{}, {Type: "User", ID: "42"}, {Type: "user", ID: " 42"}, {Type: "user", ID: "42\n"},
		{Type: "user", ID: string([]byte{0xff})}, {Type: "user", ID: strings.Repeat("a", 201)},
	} {
		if _, err := notifier.Send(t.Context(), recipient, Message{Key: "1", Type: "test", Database: json.RawMessage(`{}`)}); !errors.Is(err, ErrInvalidRecipient) {
			t.Fatal(recipient, err)
		}
	}
}
