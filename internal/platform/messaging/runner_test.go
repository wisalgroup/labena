package messaging

import (
	"context"
	"errors"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
)

func TestMissingCollaboratorsFailAtAssembly(t *testing.T) {
	if publisher, err := NewPublisher(nil); err == nil || publisher != nil {
		t.Fatal("missing transport produced a publisher", publisher, err)
	}
	if runner, err := RegisterRunner(nil); err == nil || runner != nil {
		t.Fatal("missing router produced a runner", runner, err)
	}
}

func TestEventCallsRejectLateAdmission(t *testing.T) {
	calls := &eventCalls{}
	handler := calls.middleware(func(*message.Message) ([]*message.Message, error) { t.Fatal("late handler ran"); return nil, nil })
	calls.stop()
	calls.active.Wait()
	if _, err := handler(message.NewMessage("late", nil)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
