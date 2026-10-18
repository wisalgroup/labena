package platform_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/internal/platform/dependencies"
	"github.com/wisalgroup/labena/internal/platform/mail"
	mailintegration "github.com/wisalgroup/labena/internal/platform/mail/integration"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/mailview"
)

type mailTransport func(context.Context, mail.Message) error

func (f mailTransport) Send(ctx context.Context, message mail.Message) error { return f(ctx, message) }

type mailQueue struct {
	tasks   []*asynq.Task
	options []asynq.Option
	err     error
}

func (q *mailQueue) EnqueueContext(_ context.Context, task *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	q.options = options
	if q.err != nil {
		return nil, q.err
	}
	return &asynq.TaskInfo{ID: "accepted"}, nil
}

func mailMessage() mail.Message {
	return mail.Message{To: []mail.Address{{Email: "reader@example.com"}}, Subject: "Report ready", TextBody: "Your report is ready."}
}

func TestMailImmediateWithoutQueue(t *testing.T) {
	message := mailMessage()
	message.Attachments = []mail.Attachment{mail.NewAttachment("report.txt", "text/plain", strings.NewReader("report"))}
	transportErr := errors.New("provider failed")
	calls := 0
	sender := mailTransport(func(_ context.Context, got mail.Message) error {
		calls++
		if !reflect.DeepEqual(got, message) {
			t.Fatal("immediate send changed the message or reader")
		}
		return transportErr
	})
	mailer, err := mail.New(sender, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := mailer.Send(t.Context(), message); !errors.Is(err, transportErr) || calls != 1 {
		t.Fatal(calls, err)
	}
	if _, err := mailer.Enqueue(t.Context(), mailMessage()); !errors.Is(err, mail.ErrQueueUnavailable) || calls != 1 {
		t.Fatal("enqueue fell back to sending", err)
	}
	if _, err := mail.New(nil, nil); !errors.Is(err, mail.ErrTransportRequired) {
		t.Fatal(err)
	}
}

func TestMailViewSnapshotThroughRegisteredHandler(t *testing.T) {
	views, err := mailview.New(config.Defaults().Mail.Brand("Test app"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := views.Render(mailview.Email{Subject: "Report ready", Title: "Your report", Paragraphs: []string{"Ready to read."}})
	if err != nil {
		t.Fatal(err)
	}
	message := mail.Message{From: &mail.Address{Email: "sender@example.com", Name: "Reports"},
		To: []mail.Address{{Email: "reader@example.com"}}, CC: []mail.Address{{Email: "copy@example.com"}},
		BCC: []mail.Address{{Email: "private@example.com"}}, ReplyTo: []mail.Address{{Email: "support@example.com"}},
		Subject: content.Subject, HTMLBody: content.HTML, TextBody: content.Text,
		Headers: map[string]string{"X-Report": "monthly"}, Tags: map[string]string{"kind": "report"}}
	var deliveries []mail.Message
	sender := mailTransport(func(_ context.Context, got mail.Message) error { deliveries = append(deliveries, got); return nil })
	queue := &mailQueue{}
	mailer, err := mail.New(sender, queue)
	if err != nil {
		t.Fatal(err)
	}
	options := []asynq.Option{asynq.ProcessIn(time.Minute), asynq.MaxRetry(1)}
	info, err := mailer.Enqueue(t.Context(), message, options...)
	if err != nil || info.ID != "accepted" || len(deliveries) != 0 || len(queue.tasks) != 1 {
		t.Fatal("enqueue must only acknowledge queued work", info, err)
	}
	if !reflect.DeepEqual(queue.options, options) || queue.tasks[0].Headers() == nil {
		t.Fatal("native options or trace headers lost")
	}
	mux := asynq.NewServeMux()
	if err := mailintegration.Register(mailer, dependencies.Targets{TaskRouter: mux}); err != nil {
		t.Fatal(err)
	}
	if err := mux.ProcessTask(t.Context(), queue.tasks[0]); err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 || !reflect.DeepEqual(deliveries[0], message) {
		t.Fatal("worker did not preserve the rendered message")
	}
	message.From.Name = "changed"
	message.To[0].Email = "different@example.com"
	message.Headers["X-Report"] = "changed"
	message.Tags["kind"] = "changed"
	if err := mux.ProcessTask(t.Context(), queue.tasks[0]); err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 2 || !reflect.DeepEqual(deliveries[0], deliveries[1]) || len(queue.tasks) != 1 {
		t.Fatal("retry changed the snapshot or recursively enqueued")
	}
}

func TestMailRejectsInvalidWorkBeforeEffects(t *testing.T) {
	queue := &mailQueue{}
	sender := mailTransport(func(context.Context, mail.Message) error { t.Fatal("unexpected send"); return nil })
	mailer, err := mail.New(sender, queue)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*mail.Message){
		func(m *mail.Message) { m.To = nil },
		func(m *mail.Message) { m.To[0].Email = "invalid" },
		func(m *mail.Message) { m.Subject = "injected\r\nheader" },
		func(m *mail.Message) { m.Headers = map[string]string{"X-Test": "bad\nvalue"} },
		func(m *mail.Message) { m.TextBody = strings.Repeat("x", mail.MaxPayloadSize+1) },
		func(m *mail.Message) { m.Attachments = []mail.Attachment{{Filename: "report.txt"}} },
		func(m *mail.Message) { m.CloseAttachments = true },
	} {
		message := mailMessage()
		change(&message)
		if _, err := mailer.Enqueue(t.Context(), message); err == nil {
			t.Fatal("invalid message accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := mailer.Enqueue(ctx, mailMessage()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := mailer.Send(ctx, mailMessage()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	db, err := database.Open(t.Context(), database.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "mail.db"), MaxOpenConns: 1, PingTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := database.NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Execute(t.Context(), func(ctx context.Context) error {
		if err := mailer.Send(ctx, mailMessage()); !errors.Is(err, database.ErrTransactionActive) {
			t.Fatal(err)
		}
		if _, err := mailer.Enqueue(ctx, mailMessage()); !errors.Is(err, database.ErrTransactionActive) {
			t.Fatal(err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(queue.tasks) != 0 {
		t.Fatal("rejected input reached Redis")
	}
}

func TestMailHandlerFailureContracts(t *testing.T) {
	cause := errors.New("SMTP response with private@example.com")
	calls := 0
	sender := mailTransport(func(context.Context, mail.Message) error { calls++; return cause })
	handler, err := mail.NewHandler(sender)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{"null", "{}", `{"to":[]} {}`, `{"unknown":true}`, strings.Repeat("x", mail.MaxPayloadSize+1)} {
		if err := handler.ProcessTask(t.Context(), asynq.NewTask(mail.SendTask, []byte(payload))); !errors.Is(err, asynq.SkipRetry) {
			t.Fatal(err)
		}
	}
	for _, task := range []*asynq.Task{nil, asynq.NewTask(mail.SendTask+".unknown", nil)} {
		if err := handler.ProcessTask(t.Context(), task); !errors.Is(err, asynq.SkipRetry) {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatal("malformed task reached the transport")
	}
	queue := &mailQueue{}
	mailer, err := mail.New(sender, queue)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mailer.Enqueue(t.Context(), mailMessage()); err != nil {
		t.Fatal(err)
	}
	err = handler.ProcessTask(t.Context(), queue.tasks[0])
	if calls != 1 || !errors.Is(err, cause) || errors.Is(err, asynq.SkipRetry) || strings.Contains(err.Error(), "private@example.com") {
		t.Fatal("transport cause or safe failure contract lost", err)
	}
	queue.err = errors.New("Redis unavailable")
	if _, err := mailer.Enqueue(t.Context(), mailMessage()); !errors.Is(err, queue.err) || calls != 1 {
		t.Fatal("queue failure caused an immediate send", err)
	}
}
