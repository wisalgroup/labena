package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/uptrace/bun"

	"github.com/wisalgroup/labena/internal/platform/mail"
	modulemigrations "github.com/wisalgroup/labena/internal/platform/notification/migrations"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/mailview"
	"github.com/wisalgroup/labena/pkg/pagination"
)

func databaseFixture(t *testing.T) (*bun.DB, uuid.UUID) {
	t.Helper()
	db := testdb.OpenDriver(t, database.DriverPostgres)
	var registry migrate.Registry
	for name, files := range map[string]fs.FS{"cache": testdb.Migrations(t, database.DriverPostgres), "capability": modulemigrations.Files} {
		if err := registry.Register(name, files); err != nil {
			t.Fatal(err)
		}
	}
	files, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := migrate.NewProvider(t.Context(), db, files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	if applied, err := provider.Up(t.Context()); err != nil || len(applied) != 0 {
		t.Fatal("non-idempotent baseline", err)
	}
	return db, uuid.New()
}

type queueCapture struct {
	calls   int
	failure error
	tasks   []*asynq.Task
}

func (q *queueCapture) EnqueueContext(_ context.Context, t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.calls++
	q.tasks = append(q.tasks, t)
	return &asynq.TaskInfo{}, q.failure
}

type mailCapture struct {
	calls atomic.Int32
	fail  atomic.Bool
}

func (m *mailCapture) Send(context.Context, mail.Message) error {
	m.calls.Add(1)
	if m.fail.Load() {
		return errors.New("provider unavailable")
	}
	return nil
}
func TestNotificationDeliveryLedgerPostgres(t *testing.T) {
	db, owner := databaseFixture(t)
	repo := newStore(db)
	queue := &queueCapture{}
	sender := &mailCapture{}
	sender.fail.Store(true)
	views, err := mailview.New(mailview.Brand{Name: "Test", WebsiteURL: "https://example.com", AccentColor: "#000000"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(db, Options{Queue: queue, Mailer: sender, MailRenderer: views})
	if err != nil {
		t.Fatal(err)
	}
	to := Recipient{Type: "user", ID: owner.String(), Email: "owner@example.com"}
	message := Message{Key: "order:12", Type: "order.shipped", Database: json.RawMessage(`{"title":"Shipped"}`), Email: &mailview.Email{Subject: "Shipped", Title: "Your order shipped"}}
	receipt, err := svc.Enqueue(t.Context(), to, message)
	if err != nil || len(receipt.DeliveryIDs) != 2 || queue.calls != 0 {
		t.Fatal(receipt, err)
	}
	again, err := svc.Enqueue(t.Context(), to, message)
	if err != nil || again.DeliveryIDs[0] != receipt.DeliveryIDs[0] || again.DeliveryIDs[1] != receipt.DeliveryIDs[1] {
		t.Fatal("dedup IDs changed", receipt, again, err)
	}
	// Redis being unavailable cannot remove committed intent.
	queue.failure = errors.New("redis offline")
	if _, err := svc.Dispatch(t.Context()); !errors.Is(err, queue.failure) {
		t.Fatal(err)
	}
	due, err := repo.Due(t.Context(), 100)
	if err != nil || len(due) != 2 {
		t.Fatal("lost committed intent", due, err)
	}
	queue.failure = nil
	if n, err := svc.Dispatch(t.Context()); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	for _, id := range receipt.DeliveryIDs {
		if err := svc.Deliver(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := repo.GetDelivery(t.Context(), receipt.DeliveryIDs[0])
	second, _ := repo.GetDelivery(t.Context(), receipt.DeliveryIDs[1])
	if first.Status != "sent" || second.Status != "pending" || second.Attempts != 1 || sender.calls.Load() != 1 {
		t.Fatal(first, second, sender.calls.Load())
	}
	// Duplicate task executions must not retry a not-yet-due channel or resend success.
	for _, id := range receipt.DeliveryIDs {
		if err := svc.Deliver(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	if sender.calls.Load() != 1 {
		t.Fatal("retry delay bypassed")
	}
	sender.fail.Store(false)
	if _, err := db.NewRaw("UPDATE notification_deliveries SET available_at=now() WHERE id=?", second.ID).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if err := svc.Deliver(t.Context(), second.ID); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	second, _ = repo.GetDelivery(t.Context(), second.ID)
	if second.Status != "sent" || second.Attempts != 2 || sender.calls.Load() != 2 {
		t.Fatal("duplicate provider calls", second, sender.calls.Load())
	}
	if count, err := repo.UnreadCount(t.Context(), Recipient{Type: "user", ID: owner.String()}); err != nil || count != 1 {
		t.Fatal("database notification duplicate", count, err)
	}
	// Payload snapshots and successful attempts survive duplicate publication.
	message.Database = json.RawMessage(`{"title":"Changed"}`)
	message.Email.Title = "Changed"
	if _, err := svc.Enqueue(t.Context(), to, message); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.GetDelivery(t.Context(), first.ID)
	if strings.Contains(string(stored.Payload), "Changed") || stored.Status != "sent" {
		t.Fatal("duplicate changed first occurrence")
	}
	if err := svc.RetryDelivery(t.Context(), first.ID); !errors.Is(err, ErrDeliveryNotFailed) {
		t.Fatal(err)
	}
	// Immediate external effects reject a SQL transaction before any channel runs.
	guardTx, _ := database.NewTxManager(db, nil)
	if err := guardTx.Execute(t.Context(), func(ctx context.Context) error { _, err := svc.Send(ctx, to, message); return err }); !errors.Is(err, database.ErrTransactionActive) {
		t.Fatal("mail allowed inside transaction", err)
	}
	page, err := svc.ListDeliveries(t.Context(), "sent", pagination.NewPagination(1, 1))
	if err != nil || page.Total != 2 || len(page.Items) != 1 || len(page.Items[0].Payload) != 0 {
		t.Fatal("operator listing leaked payload or ignored pagination", page, err)
	}
	metadata, err := svc.GetDelivery(t.Context(), first.ID)
	if err != nil || len(metadata.Payload) != 0 || metadata.LeaseToken != nil {
		t.Fatal("operator detail leaked delivery content", err)
	}
	// Caller rollback removes all new channel intents with the business transaction.
	tx, _ := database.NewTxManager(db, nil)
	abort := errors.New("abort")
	message.Key = "rollback"
	err = tx.Execute(t.Context(), func(ctx context.Context) error {
		if _, err := svc.Enqueue(ctx, to, message); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	var count int
	if err := db.NewRaw("SELECT count(*) FROM notification_deliveries WHERE key='rollback'").Scan(t.Context(), &count); err != nil || count != 0 {
		t.Fatal("escaped transaction", count, err)
	}
}
func TestNotificationLeaseRecoveryAndBoundedRetriesPostgres(t *testing.T) {
	db, owner := databaseFixture(t)
	repo := newStore(db)
	sender := &mailCapture{}
	sender.fail.Store(true)
	views, _ := mailview.New(mailview.Brand{Name: "Test", WebsiteURL: "https://example.com", AccentColor: "#000000"})
	svc, _ := New(db, Options{Queue: &queueCapture{}, Mailer: sender, MailRenderer: views})
	receipt, err := svc.Enqueue(t.Context(), Recipient{Type: "user", ID: owner.String(), Email: "owner@example.com"}, Message{Key: "failure", Type: "test", Email: &mailview.Email{Subject: "Test", Title: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	id := receipt.DeliveryIDs[0]
	stale, err := repo.Claim(t.Context(), id)
	if err != nil || stale == nil {
		t.Fatal(err)
	}
	if _, err := db.NewRaw("UPDATE notification_deliveries SET lease_until=now()-interval '1 second' WHERE id=?", id).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := repo.Claim(t.Context(), id)
	if err != nil || current == nil {
		t.Fatal(err)
	}
	if err := repo.Finish(t.Context(), stale, true); !errors.Is(err, ErrDeliveryClaimLost) {
		t.Fatal("stale claim accepted", err)
	}
	if err := repo.Finish(t.Context(), current, false); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if _, err := db.NewRaw("UPDATE notification_deliveries SET available_at=now() WHERE id=?", id).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := svc.Deliver(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	row, _ := repo.GetDelivery(t.Context(), id)
	if row.Status != "failed" || row.Attempts != 8 {
		t.Fatal(row)
	}
	if due, err := repo.Due(t.Context(), 100); err != nil || len(due) != 0 {
		t.Fatal(due, err)
	}
	if err := svc.RetryDelivery(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	sender.fail.Store(false)
	if err := svc.Deliver(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	row, _ = repo.GetDelivery(t.Context(), id)
	if row.Status != "sent" || row.Attempts != 1 {
		t.Fatal(row)
	}
}
func TestNotificationReadinessUsesDatabaseClock(t *testing.T) {
	db, owner := databaseFixture(t)
	repo := newStore(db)
	future := time.Now().Add(24 * time.Hour)
	row := &Delivery{ID: uuid.New(), RecipientType: "user", RecipientID: owner.String(), Key: "clock-skew", Type: "test", Channel: ChannelEmail, Payload: []byte(`{}`), AvailableAt: future, CreatedAt: future, UpdatedAt: future}
	if _, err := repo.StoreDeliveries(t.Context(), []*Delivery{row}); err != nil {
		t.Fatal(err)
	}
	due, err := repo.Due(t.Context(), 100)
	if err != nil || len(due) != 1 || due[0] != row.ID {
		t.Fatal("application time delayed immediately due work", due, err)
	}
}

func TestPolymorphicDeliveryAndDatabaseNotificationIsolation(t *testing.T) {
	db, _ := databaseFixture(t)
	notifier, err := New(db, Options{Queue: &queueCapture{}})
	if err != nil {
		t.Fatal(err)
	}
	recipients := []Recipient{{Type: "user", ID: "42"}, {Type: "admin", ID: "42"}, {Type: "order", ID: "order-A"}}
	ids := map[uuid.UUID]bool{}
	for _, to := range recipients {
		msg := Message{Key: "same-occurrence", Type: "update", Database: json.RawMessage(`{"title":"Original"}`)}
		var wg sync.WaitGroup
		receipts := make(chan Receipt, 8)
		for range 8 {
			wg.Go(func() {
				receipt, err := notifier.Enqueue(t.Context(), to, msg)
				if err != nil {
					t.Error(err)
					return
				}
				receipts <- receipt
			})
		}
		wg.Wait()
		close(receipts)
		var deliveryID uuid.UUID
		for receipt := range receipts {
			if len(receipt.DeliveryIDs) != 1 {
				t.Fatal(receipt)
			}
			if deliveryID != uuid.Nil && deliveryID != receipt.DeliveryIDs[0] {
				t.Fatal("duplicate occurrence escaped uniqueness")
			}
			deliveryID = receipt.DeliveryIDs[0]
		}
		if ids[deliveryID] {
			t.Fatal("recipient types shared a delivery")
		}
		ids[deliveryID] = true
		if err := notifier.Deliver(t.Context(), deliveryID); err != nil {
			t.Fatal(err)
		}
		if n, err := notifier.UnreadCount(t.Context(), to); err != nil || n != 1 {
			t.Fatal(to, n, err)
		}
	}
	user, err := notifier.List(t.Context(), recipients[0], pagination.NewPagination(1, 20))
	if err != nil {
		t.Fatal(err)
	}
	if err := notifier.MarkRead(t.Context(), recipients[1], user.Items[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-type read", err)
	}
	if err := notifier.MarkRead(t.Context(), recipients[0], user.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	if n, err := notifier.UnreadCount(t.Context(), recipients[1]); err != nil || n != 1 {
		t.Fatal("read leaked across types", n, err)
	}
}

func TestDatabaseNotificationWriteRollbackAndDatabaseClock(t *testing.T) {
	db, _ := databaseFixture(t)
	to := Recipient{Type: "admin", ID: "42"}
	notifier, err := New(db, Options{})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	abort := errors.New("abort")
	err = tx.Execute(t.Context(), func(ctx context.Context) error {
		if _, err := notifier.Send(ctx, to, Message{Key: "rollback", Type: "test", Database: json.RawMessage(`{"title":"test"}`)}); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if n, err := notifier.UnreadCount(t.Context(), to); err != nil || n != 0 {
		t.Fatal("record escaped rollback", n, err)
	}
	n, err := newRecord(to, "test", "clock", json.RawMessage(`{"title":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	n.CreatedAt = time.Now().Add(24 * time.Hour)
	n.UpdatedAt = n.CreatedAt
	if err := newStore(db).Create(t.Context(), n); err != nil {
		t.Fatal(err)
	}
	if err := notifier.MarkRead(t.Context(), to, n.ID); err != nil {
		t.Fatal("application clock broke read transition", err)
	}
	page, err := notifier.List(t.Context(), to, pagination.NewPagination(1, 20))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].validate() != nil || page.Items[0].ReadAt == nil {
		t.Fatal("invalid stored timestamps", page)
	}
}

func TestJSONNotificationsAndQueueIndependentEnqueue(t *testing.T) {
	db, _ := databaseFixture(t)
	notifier, err := New(db, Options{})
	if err != nil {
		t.Fatal(err)
	}
	to := Recipient{Type: "admin", ID: "42", Email: "private@example.com"}
	data := json.RawMessage(`{"title":"Approved","amount":9007199254740993,"nested":{"ok":true},"items":[1,null,"x"]}`)
	message := Message{Key: "approval:42", Type: "approval.completed", Database: data}
	receipt, err := notifier.Enqueue(t.Context(), to, message)
	if err != nil || len(receipt.DeliveryIDs) != 1 {
		t.Fatal("enqueue requires a queue client", err)
	}
	// Mutating caller content after acceptance must not change persisted intent.
	data[2] = 'X'
	if _, err := notifier.Dispatch(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := notifier.Deliver(t.Context(), receipt.DeliveryIDs[0]); err != nil {
		t.Fatal(err)
	}
	page, err := notifier.List(t.Context(), to, pagination.NewPagination(1, 20))
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	record := page.Items[0]
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(record.Data, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["amount"]) != "9007199254740993" || string(fields["title"]) != `"Approved"` || record.Type != message.Type || record.Recipient.Email != "" {
		t.Fatal("payload changed", record)
	}
	var delivery Delivery
	if err := db.NewSelect().Model(&delivery).Where("id = ?", receipt.DeliveryIDs[0]).Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	if delivery.Channel != ChannelDatabase || delivery.Status != DeliverySent || delivery.Type != message.Type || strings.Contains(string(delivery.Payload), to.Email) {
		t.Fatal("database channel retained email or lost routing", delivery)
	}
}

func TestAllChannelValidationPrecedesDatabaseWrites(t *testing.T) {
	db, _ := databaseFixture(t)
	views, _ := mailview.New(mailview.Brand{Name: "Test", WebsiteURL: "https://example.com", AccentColor: "#000000"})
	mailer := &sender{}
	notifier, _ := New(db, Options{Mailer: mailer, MailRenderer: views})
	to := Recipient{Type: "user", ID: "42", Email: "invalid"}
	message := Message{Key: "welcome", Type: "welcome", Database: json.RawMessage(`{"title":"Welcome"}`), Email: &mailview.Email{Subject: "Welcome", Title: "Welcome"}}
	if _, err := notifier.Send(t.Context(), to, message); !errors.Is(err, ErrInvalidMessage) {
		t.Fatal(err)
	}
	if _, err := notifier.Enqueue(t.Context(), to, message); !errors.Is(err, ErrInvalidMessage) {
		t.Fatal(err)
	}
	if count, err := notifier.UnreadCount(t.Context(), to); err != nil || count != 0 || mailer.calls != 0 {
		t.Fatal("validation caused effects", count, err)
	}
	page, err := notifier.ListDeliveries(t.Context(), "", pagination.NewPagination(1, 20))
	if err != nil || page.Total != 0 {
		t.Fatal("validation persisted partial intent", page, err)
	}
}

func TestQueuedJSONSurvivesDatabaseNormalization(t *testing.T) {
	db, _ := databaseFixture(t)
	notifier, _ := New(db, Options{})
	to := Recipient{Type: "user", ID: "42"}
	// Compact input is below 32 KiB; PostgreSQL adds a space after each comma.
	data := json.RawMessage(`{"values":[` + strings.Repeat("0,", 14000) + `0]}`)
	if len(data) > MaxDataSize {
		t.Fatal("invalid fixture")
	}
	receipt, err := notifier.Enqueue(t.Context(), to, Message{Key: "large", Type: "report.ready", Database: data})
	if err != nil {
		t.Fatal(err)
	}
	if err := notifier.Deliver(t.Context(), receipt.DeliveryIDs[0]); err != nil {
		t.Fatal(err)
	}
	page, err := notifier.List(t.Context(), to, pagination.NewPagination(1, 20))
	if err != nil || len(page.Items) != 1 {
		t.Fatal("accepted JSON failed after normalization", page, err)
	}
	var decoded struct {
		Values []int `json:"values"`
	}
	if err := json.Unmarshal(page.Items[0].Data, &decoded); err != nil || len(decoded.Values) != 14001 {
		t.Fatal(err, len(decoded.Values))
	}
	// A small exponent spelling must not create an unbounded stored payload.
	_, err = notifier.Enqueue(t.Context(), to, Message{Key: "expanded", Type: "report.ready", Database: json.RawMessage(`{"number":1e70000}`)})
	if err == nil {
		t.Fatal("unbounded numeric expansion accepted")
	}
	var count int
	if err := db.NewRaw("SELECT count(*) FROM notification_deliveries WHERE key='expanded'").Scan(t.Context(), &count); err != nil || count != 0 {
		t.Fatal("oversized intent persisted", count, err)
	}
}

func TestOmittedKeyCreatesIndependentOccurrences(t *testing.T) {
	for _, queued := range []bool{false, true} {
		name := "send"
		if queued {
			name = "enqueue"
		}
		t.Run(name, func(t *testing.T) {
			db, _ := databaseFixture(t)
			views, _ := mailview.New(mailview.Brand{Name: "Test", WebsiteURL: "https://example.com", AccentColor: "#000000"})
			mailer := &mailCapture{}
			notifier, err := New(db, Options{Mailer: mailer, MailRenderer: views})
			if err != nil {
				t.Fatal(err)
			}
			to := Recipient{Type: "user", ID: "42", Email: "user@example.com"}
			message := Message{Type: "announcement.published", Database: json.RawMessage(`{"title":"News"}`), Email: &mailview.Email{Subject: "News", Title: "News"}}
			keys := make(map[string]bool)
			for range 2 {
				if queued {
					receipt, err := notifier.Enqueue(t.Context(), to, message)
					if err != nil || len(receipt.DeliveryIDs) != 2 {
						t.Fatal(receipt, err)
					}
					var occurrenceKey string
					for _, id := range receipt.DeliveryIDs {
						delivery, err := notifier.GetDelivery(t.Context(), id)
						if err != nil {
							t.Fatal(err)
						}
						if _, err := uuid.Parse(delivery.Key); err != nil {
							t.Fatal("missing generated key", err)
						}
						if occurrenceKey != "" && delivery.Key != occurrenceKey {
							t.Fatal("channels have different occurrence keys")
						}
						occurrenceKey = delivery.Key
						for range 2 {
							if err := notifier.Deliver(t.Context(), id); err != nil {
								t.Fatal(err)
							}
						}
					}
					if keys[occurrenceKey] {
						t.Fatal("separate calls reused a generated key")
					}
					keys[occurrenceKey] = true
				} else {
					if results, err := notifier.Send(t.Context(), to, message); err != nil || len(results) != 2 {
						t.Fatal(results, err)
					}
				}
			}
			if message.Key != "" {
				t.Fatal("caller message was mutated")
			}
			page, err := notifier.List(t.Context(), to, pagination.NewPagination(1, 20))
			if err != nil || len(page.Items) != 2 || mailer.calls.Load() != 2 {
				t.Fatal("occurrences deduplicated or worker replay resent mail", page, err, mailer.calls.Load())
			}
			if page.Items[0].Key == "" || page.Items[0].Key == page.Items[1].Key {
				t.Fatal("records lost distinct generated keys")
			}
			if queued && (!keys[page.Items[0].Key] || !keys[page.Items[1].Key]) {
				t.Fatal("worker replaced persisted occurrence keys")
			}
		})
	}
}
