package notifications

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestWelcomeKeepsOccurrenceStableAcrossProfileChanges(t *testing.T) {
	first, second := NewWelcome("Ada"), NewWelcome("Updated name")
	if first.Key != "welcome" || first.Key != second.Key || first.Database == nil || first.Email == nil {
		t.Fatal("missing occurrence/channel content")
	}
	first.Database[2] = 'X'
	first.Email.Paragraphs[0] = "changed"
	if bytes.Equal(first.Database, second.Database) || !json.Valid(second.Database) || second.Email.Paragraphs[0] == "changed" {
		t.Fatal("builders share mutable content")
	}
}
