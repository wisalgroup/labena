package notification

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/mailview"
)

type sender struct {
	calls   int
	message mail.Message
	err     error
}

func (s *sender) Send(_ context.Context, m mail.Message) error {
	s.calls++
	s.message = m
	return s.err
}

func TestEmailOnlySendAndPartialChannelResults(t *testing.T) {
	views, err := mailview.New(mailview.Brand{Name: "Test", WebsiteURL: "https://example.com", AccentColor: "#000000"})
	if err != nil {
		t.Fatal(err)
	}
	mailer := &sender{}
	if _, err := New(nil, Options{Mailer: mailer}); !errors.Is(err, ErrMailRendererRequired) {
		t.Fatal(err)
	}
	notifier, err := New(nil, Options{Mailer: mailer, MailRenderer: views})
	if err != nil {
		t.Fatal(err)
	}
	message := Message{Key: "invoice:1", Type: "invoice.paid", Database: json.RawMessage(`{"amount":42}`), Email: &mailview.Email{Subject: "Paid", Title: "<script>bad</script>", Paragraphs: []string{"Your payment is recorded."}}}
	to := Recipient{Type: "user", ID: "42", Email: "owner@example.com"}
	results, err := notifier.Send(t.Context(), to, message)
	if !errors.Is(err, database.ErrNilDB) || len(results) != 2 || results[0].Channel != ChannelDatabase || results[0].Err == nil || results[1].Channel != ChannelEmail || results[1].Err != nil || mailer.calls != 1 {
		t.Fatal(results, err)
	}
	if mailer.message.Subject != "Paid" || mailer.message.TextBody == "" || strings.Contains(mailer.message.HTMLBody, "<script>") {
		t.Fatal("mail views bypassed")
	}
	message.Database = nil
	if _, err := notifier.Send(t.Context(), to, message); err != nil || mailer.calls != 2 {
		t.Fatal(err)
	}
	to.Email = "invalid"
	if _, err := notifier.Send(t.Context(), to, message); !errors.Is(err, ErrInvalidMessage) || mailer.calls != 2 {
		t.Fatal("invalid address caused effects", err)
	}
}

func TestInvalidDatabasePayloadNeverSendsEmail(t *testing.T) {
	views, _ := mailview.New(mailview.Brand{Name: "Test", WebsiteURL: "https://example.com", AccentColor: "#000000"})
	mailer := &sender{}
	notifier, _ := New(nil, Options{Mailer: mailer, MailRenderer: views})
	message := Message{Key: "welcome", Type: "welcome", Database: json.RawMessage(`null`), Email: &mailview.Email{Subject: "Welcome", Title: "Welcome"}}
	if _, err := notifier.Send(t.Context(), Recipient{Type: "user", ID: "42", Email: "user@example.com"}, message); !errors.Is(err, ErrInvalidData) || mailer.calls != 0 {
		t.Fatal(err, mailer.calls)
	}
}
