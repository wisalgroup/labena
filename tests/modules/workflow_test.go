package modules_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/internal/modules/identity"
	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	tasks "github.com/wisalgroup/labena/internal/modules/identity/jobs"
	"github.com/wisalgroup/labena/internal/modules/identity/notifications"
	identityrepo "github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/events"
	sharedmigrations "github.com/wisalgroup/labena/internal/modules/shared/migrations"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/internal/platform/dependencies"
	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/internal/platform/media"
	mediaintegration "github.com/wisalgroup/labena/internal/platform/media/integration"
	"github.com/wisalgroup/labena/internal/platform/messaging"
	"github.com/wisalgroup/labena/internal/platform/notification"
	notificationintegration "github.com/wisalgroup/labena/internal/platform/notification/integration"
	"github.com/wisalgroup/labena/internal/platform/testdb"
	asynqmiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/mailview"
	"github.com/wisalgroup/labena/pkg/pagination"
	"github.com/wisalgroup/labena/pkg/token"
)

// Exercise actual migrations, HTTP registration, a business-owned Watermill
// local subscriber, Redis task delivery, and idempotent PostgreSQL writes together.
func TestModulesPostgresRedisWorkflow(t *testing.T) {
	redisURL := os.Getenv("QUEUE_TEST_REDIS")
	if redisURL == "" {
		t.Skip("QUEUE_TEST_REDIS not set")
	}
	db := testdb.OpenDriver(t, database.DriverPostgres)
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	pool := redis.NewClient(options)
	defer pool.Close()
	client := asynq.NewClientFromRedisClient(pool)
	queueName := "modules_" + strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")
	inspector := asynq.NewInspectorFromRedisClient(pool)
	defer inspector.DeleteQueue(queueName, true)
	logger := watermill.NopLogger{}
	bus := gochannel.NewGoChannel(gochannel.Config{BlockPublishUntilSubscriberAck: true}, logger)
	defer bus.Close()
	router, err := message.NewRouter(message.RouterConfig{CloseTimeout: 3 * time.Second}, logger)
	if err != nil {
		t.Fatal(err)
	}
	httpRouter := newAPI(t)

	queue := asynqmiddleware.EnqueueFunc(func(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
		opts = append(opts, asynq.Queue(queueName), asynq.MaxRetry(0))
		return client.EnqueueContext(ctx, task, opts...)
	})
	publisher, err := messaging.NewPublisher(bus)
	if err != nil {
		t.Fatal(err)
	}
	console, err := cli.New(cli.Config{})
	if err != nil {
		t.Fatal(err)
	}
	registry := &migrate.Registry{}
	targets := dependencies.Targets{Migrations: registry, RootCommand: console, HTTP: httpRouter, TaskRouter: asynq.NewServeMux(), EventRouter: router, EventSubscriber: bus}
	sender := &capturedMail{}
	cfg := config.Defaults()
	views, err := mailview.New(cfg.Mail.Brand(cfg.App.AppName))
	if err != nil {
		t.Fatal(err)
	}
	transactions, err := database.NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	mailer, err := mail.New(sender, queue)
	if err != nil {
		t.Fatal(err)
	}
	resources := dependencies.Resources{DB: db, Transactions: transactions, Queue: queue, EventPublisher: publisher, Mailer: mailer, MailRenderer: views, RateLimiter: testdb.RateLimiter(t)}
	notifier, err := notification.New(resources.DB, notification.Options{Queue: resources.Queue, Mailer: resources.Mailer, MailRenderer: resources.MailRenderer})
	if err != nil {
		t.Fatal(err)
	}
	resources.Notifier = notifier
	access, err := token.NewAccess([]byte(strings.Repeat("a", 32)), "modules-test", "api")
	if err != nil {
		t.Fatal(err)
	}
	authentication, err := service.NewAuthService(service.AuthOptions{
		VerificationRepository: identityrepo.NewVerificationRepository(db),
		SessionRepository:      identityrepo.NewSessionRepository(db),
		VerificationKey:        []byte(strings.Repeat("x", 32)),
		VerificationCodePolicy: domain.DefaultVerificationCodePolicy(),
		RateLimitPolicy:        domain.DefaultAuthRateLimitPolicy(),
		SessionPolicy:          domain.SessionPolicy{AccessTokenLifetime: time.Hour, AllowConcurrentSessions: true},
		Access:                 access,
		Notifier:               notifier, Transactions: transactions,
		Mailer: mailer, MailRenderer: views, Limiter: resources.RateLimiter, EventPublisher: publisher,
	})
	if err != nil {
		t.Fatal(err)
	}
	targets.AuthenticatedHTTP = api.Authenticated(targets.HTTP, authentication)
	uploads, err := media.New(resources.DB, resources.Disks, resources.DefaultDisk, media.Config{})
	if err != nil {
		t.Fatal(err)
	}
	resources.Media = uploads
	if err := identity.Register(resources, targets, authentication, false); err != nil {
		t.Fatal(err)
	}
	if err := notificationintegration.Register(notifier, targets); err != nil {
		t.Fatal(err)
	}
	if err := mediaintegration.Register(uploads, targets); err != nil {
		t.Fatal(err)
	}
	if err := cli.Validate(console); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("internal/modules/shared/migrations", sharedmigrations.Files); err != nil {
		t.Fatal(err)
	}
	// Include the cache history already installed by the isolated-schema helper.
	if err = registry.Register("test/cache", testdb.Migrations(t, database.DriverPostgres)); err != nil {
		t.Fatal(err)
	}
	files, err := registry.FS(database.DriverPostgres)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := migrate.NewProvider(t.Context(), db, files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	if changed, err := provider.Up(t.Context()); err != nil || len(changed) != 0 {
		t.Fatal("migrations not repeatable", changed, err)
	}
	var retiredTables int
	if err := db.NewRaw(`SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename IN ('watermill_identity.user_registered.v1', 'watermill_offsets_identity.user_registered.v1')`).Scan(t.Context(), &retiredTables); err != nil || retiredTables != 0 {
		t.Fatal("retired event tables remain after migrations", retiredTables, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	routerDone := make(chan error, 1)
	go func() { routerDone <- router.Run(ctx) }()
	select {
	case <-router.Running():
	case err := <-routerDone:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer func() {
		if err := router.Close(); err != nil {
			t.Error(err)
		}
		if err := <-routerDone; err != nil {
			t.Error(err)
		}
	}()
	worker := asynq.NewServerFromRedisClient(pool, asynq.Config{Concurrency: 2, Queues: map[string]int{queueName: 1}, ShutdownTimeout: time.Second, LogLevel: asynq.ErrorLevel})
	if err = worker.Start(targets.TaskRouter); err != nil {
		t.Fatal(err)
	}
	defer worker.Shutdown()
	// Authentication registers OTP cleanup alongside session cleanup.
	if command, _, err := console.Find([]string{"identity", "prune-verification"}); err != nil || command.Name() != "prune-verification" {
		t.Fatal("missing challenge cleanup", err)
	}
	unauthenticated := httptest.NewRecorder()
	httpRouter.ServeHTTP(unauthenticated, httptest.NewRequest("GET", "/notifications", nil))
	if unauthenticated.Code != 401 {
		t.Fatal("notifier exposed before authentication", unauthenticated.Code)
	}
	var challenge struct {
		ID uuid.UUID `json:"challenge_id"`
	}
	signInRequest := httptest.NewRecorder()
	httpRouter.ServeHTTP(signInRequest, httptest.NewRequest("POST", "/auth/otp/send", strings.NewReader(`{"email":"ada@example.com"}`)).WithContext(ctx))
	if signInRequest.Code != 202 {
		t.Fatal(signInRequest.Code, signInRequest.Body.String())
	}
	if err := decodeResponseData(signInRequest.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	code := regexp.MustCompile(`\b[0-9]{6}\b`).FindString(sender.lastCode().TextBody)
	if sender.lastCode().Subject != "Your sign-in code" {
		t.Fatal("wrong email template")
	}
	loginJSON, _ := json.Marshal(map[string]string{"challenge_id": challenge.ID.String(), "code": code, "platform": "web", "device_name": "Integration browser"})
	signIn := httptest.NewRecorder()
	httpRouter.ServeHTTP(signIn, httptest.NewRequest("POST", "/auth/otp/verify", strings.NewReader(string(loginJSON))).WithContext(ctx))
	if signIn.Code != 201 {
		t.Fatal(signIn.Code, signIn.Body.String())
	}
	var credentials struct {
		User struct {
			ID              uuid.UUID `json:"id"`
			Name            string    `json:"name"`
			ProfileComplete bool      `json:"profile_complete"`
		} `json:"user"`
		Access    string    `json:"access_token"`
		Refresh   string    `json:"refresh_token"`
		SessionID uuid.UUID `json:"session_id"`
	}
	if err := decodeResponseData(signIn.Body.Bytes(), &credentials); err != nil || credentials.Access == "" || credentials.Refresh == "" {
		t.Fatal("missing credentials", err)
	}
	if signIn.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("tokens cacheable")
	}
	if credentials.User.ID == uuid.Nil || credentials.User.Name != "" || credentials.User.ProfileComplete {
		t.Fatal("new profile should be incomplete")
	}
	userID := credentials.User.ID
	account, err := identityrepo.NewUserRepository(db).Get(ctx, userID)
	if err != nil || account.EmailVerifiedAt == nil {
		t.Fatal("email not verified", err)
	}
	// A returning user keeps the same incomplete profile and receives 200.
	for _, complete := range []bool{false, true} {
		if complete {
			update := httptest.NewRequest("PATCH", "/users/me", strings.NewReader(`{"name":"Ada"}`)).WithContext(ctx)
			update.Header.Set("Authorization", "Bearer "+credentials.Access)
			out := httptest.NewRecorder()
			httpRouter.ServeHTTP(out, update)
			if out.Code != 200 || !strings.Contains(out.Body.String(), `"profile_complete":true`) {
				t.Fatal("profile update", out.Code, out.Body.String())
			}
		}
		if _, err := db.NewRaw("UPDATE verification_challenges SET created_at = ? WHERE destination = ?", time.Now().Add(-2*time.Minute), "ada@example.com").Exec(ctx); err != nil {
			t.Fatal(err)
		}
		out := httptest.NewRecorder()
		httpRouter.ServeHTTP(out, httptest.NewRequest("POST", "/auth/otp/send", strings.NewReader(`{"email":"ada@example.com"}`)).WithContext(ctx))
		if out.Code != 202 {
			t.Fatal(out.Code, out.Body.String())
		}
		if err := decodeResponseData(out.Body.Bytes(), &challenge); err != nil {
			t.Fatal(err)
		}
		code = regexp.MustCompile(`\b[0-9]{6}\b`).FindString(sender.lastCode().TextBody)
		payload, _ := json.Marshal(map[string]string{"challenge_id": challenge.ID.String(), "code": code, "platform": "web"})
		out = httptest.NewRecorder()
		httpRouter.ServeHTTP(out, httptest.NewRequest("POST", "/auth/otp/verify", strings.NewReader(string(payload))).WithContext(ctx))
		if out.Code != 200 {
			t.Fatal("returning user", out.Code, out.Body.String())
		}
		if err := decodeResponseData(out.Body.Bytes(), &credentials); err != nil || credentials.User.ID != userID || credentials.User.ProfileComplete != complete {
			t.Fatal("returning profile", err, out.Body.String())
		}
	}
	accessToken := credentials.Access
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := notifier.Dispatch(ctx); err != nil {
			t.Fatal(err)
		}
		page, err := notifier.List(ctx, notification.Recipient{Type: "user", ID: userID.String()}, pagination.NewPagination(1, 50))
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("signup reactions not processed", ctx.Err())
		case <-ticker.C:
		}
	}
	// Duplicate local publication must leave the business effect idempotent.
	event := events.NewUserRegistered(userID)
	if err := publisher.PublishWithID(ctx, events.UserRegisteredType, event.ID.String(), event); err != nil {
		t.Fatal(err)
	}

	// Explicitly queued work still uses native Asynq and the same idempotent
	// operation. Retention lets this test observe completed delivery deterministically.
	task, err := tasks.NewWelcome(userID)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := queue.EnqueueContext(ctx, task, asynq.Retention(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for {
		info, err := inspector.GetTaskInfo(queueName, queued.ID)
		if err != nil {
			t.Fatal(err)
		}
		if info.State == asynq.TaskStateCompleted {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("explicit task was not processed", ctx.Err())
		case <-ticker.C:
		}
	}
	// The generic delivery ledger submits committed IDs through the real Redis
	// client, and the registered platform handler executes them in the worker.
	probeMessage := databaseWelcome()
	probeMessage.Key = "delivery-probe"
	receipt, err := notifier.Enqueue(ctx, notification.Recipient{Type: "user", ID: userID.String()}, probeMessage)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := notifier.Dispatch(ctx); err != nil {
		t.Fatal(n, err)
	}
	for {
		delivery, err := notifier.GetDelivery(ctx, receipt.DeliveryIDs[0])
		if err != nil {
			t.Fatal(err)
		}
		if delivery.Status == "sent" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("platform delivery not processed", ctx.Err())
		case <-ticker.C:
		}
	}
	// Concurrent retries must converge on one notifier occurrence.
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			task, _ := tasks.NewWelcome(userID)
			if err := targets.TaskRouter.ProcessTask(ctx, task); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	page, err := notifier.List(ctx, notification.Recipient{Type: "user", ID: userID.String()}, pagination.NewPagination(1, 50))
	items := page.Items
	if err != nil || len(items) != 2 {
		t.Fatal("duplicate notification", items, err)
	}
	for _, path := range []string{"/users/me", "/users/" + userID.String(), "/media", "/notifications", "/notifications/unread-count", "/auth/sessions"} {
		out := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		httpRouter.ServeHTTP(out, req)
		if out.Code != 200 {
			t.Fatal(path, out.Code, out.Body.String())
		}
	}
	// A valid token still cannot select a different recipient via the URL.
	for _, path := range []string{"/users/" + uuid.NewString(), "/media/" + uuid.NewString()} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		out := httptest.NewRecorder()
		httpRouter.ServeHTTP(out, req)
		if out.Code != 404 {
			t.Fatal("cross-user access", out.Code)
		}
	}
	read := httptest.NewRecorder()
	readReq := httptest.NewRequest("PATCH", "/notifications/"+items[0].ID.String()+"/read", nil)
	readReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpRouter.ServeHTTP(read, readReq)
	if read.Code != 204 {
		t.Fatal(read.Code, read.Body.String())
	}
	refreshed := httptest.NewRecorder()
	refreshJSON, _ := json.Marshal(map[string]string{"refresh_token": credentials.Refresh})
	httpRouter.ServeHTTP(refreshed, httptest.NewRequest("POST", "/auth/refresh", strings.NewReader(string(refreshJSON))))
	if refreshed.Code != 200 {
		t.Fatal(refreshed.Code, refreshed.Body.String())
	}
	if err := decodeResponseData(refreshed.Body.Bytes(), &credentials); err != nil {
		t.Fatal(err)
	}
	logout := httptest.NewRecorder()
	logoutReq := httptest.NewRequest("POST", "/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+credentials.Access)
	httpRouter.ServeHTTP(logout, logoutReq)
	if logout.Code != 204 {
		t.Fatal(logout.Code)
	}
	denied := httptest.NewRecorder()
	deniedReq := httptest.NewRequest("GET", "/notifications", nil)
	deniedReq.Header.Set("Authorization", "Bearer "+credentials.Access)
	httpRouter.ServeHTTP(denied, deniedReq)
	if denied.Code != 401 {
		t.Fatal("logout left access valid", denied.Code)
	}
	// Every repository must participate in caller-owned transactions.
	tx, err := database.NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback probe")
	email, err := valueobjects.NewEmail("rollback@example.com")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := entities.NewUser("Rollback", email)
	if err != nil {
		t.Fatal(err)
	}
	users := identityrepo.NewUserRepository(db)
	err = tx.Execute(ctx, func(ctx context.Context) error {
		if err := users.Create(ctx, probe); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, err = users.Get(ctx, probe.ID); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatal("repository escaped transaction", err)
	}
	// A business write and a persisted notification intent share one transaction.
	databaseNotifier, err := notification.New(db, notification.Options{Queue: queue})
	if err != nil {
		t.Fatal(err)
	}
	err = tx.Execute(ctx, func(ctx context.Context) error {
		if err := users.Create(ctx, probe); err != nil {
			return err
		}
		if _, err := databaseNotifier.Enqueue(ctx, notification.Recipient{Type: "user", ID: probe.ID.String()}, databaseWelcome()); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var intents int
	if err := db.NewRaw("SELECT count(*) FROM notification_deliveries WHERE recipient_type='user' AND recipient_id=?", probe.ID.String()).Scan(ctx, &intents); err != nil || intents != 0 {
		t.Fatal("delivery escaped rollback", intents, err)
	}
	// The same email OTP endpoints also register a new user, after proof only.
	otpRequest := httptest.NewRecorder()
	httpRouter.ServeHTTP(otpRequest, httptest.NewRequest("POST", "/auth/otp/send", strings.NewReader(`{"email":"otp-new@example.com"}`)).WithContext(ctx))
	if otpRequest.Code != 202 {
		t.Fatal(otpRequest.Code, otpRequest.Body.String())
	}
	if err := decodeResponseData(otpRequest.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	exists, err := db.NewSelect().Model((*entities.User)(nil)).Where("email = ?", "otp-new@example.com").Exists(ctx)
	if err != nil || exists {
		t.Fatal("account exists before confirmation", err)
	}
	code = regexp.MustCompile(`\b[0-9]{6}\b`).FindString(sender.lastCode().TextBody)
	signupJSON, _ := json.Marshal(map[string]string{"challenge_id": challenge.ID.String(), "code": code, "platform": "web", "name": "OTP Member"})
	signup := httptest.NewRecorder()
	httpRouter.ServeHTTP(signup, httptest.NewRequest("POST", "/auth/otp/verify", strings.NewReader(string(signupJSON))).WithContext(ctx))
	if signup.Code != 201 {
		t.Fatal(signup.Code, signup.Body.String())
	}
	if err := decodeResponseData(signup.Body.Bytes(), &credentials); err != nil {
		t.Fatal(err)
	}
	me := httptest.NewRecorder()
	meReq := httptest.NewRequest("GET", "/users/me", nil).WithContext(ctx)
	meReq.Header.Set("Authorization", "Bearer "+credentials.Access)
	httpRouter.ServeHTTP(me, meReq)
	if me.Code != 200 || !strings.Contains(me.Body.String(), "OTP Member") {
		t.Fatal(me.Code, me.Body.String())
	}
	var newUser entities.User
	if err := db.NewSelect().Model(&newUser).Where("email = ?", "otp-new@example.com").Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if newUser.EmailVerifiedAt == nil {
		t.Fatal("OTP signup email not verified")
	}
	// Registration reactions are asynchronous; wait with the existing bounded context.
	for {
		if _, err := notifier.Dispatch(ctx); err != nil {
			t.Fatal(err)
		}
		count, err := notifier.UnreadCount(ctx, notification.Recipient{Type: "user", ID: newUser.ID.String()})
		if err != nil {
			t.Fatal(err)
		}
		var delivered int
		if err := db.NewRaw("SELECT count(*) FROM notification_deliveries WHERE recipient_type='user' AND recipient_id=? AND key='welcome' AND status='sent'", newUser.ID.String()).Scan(ctx, &delivered); err != nil {
			t.Fatal(err)
		}
		if count == 1 && delivered == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("OTP registration event was not processed", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	welcome := sender.welcomeFor(newUser.Email.String())
	if welcome.Subject != "Welcome to your account" || !strings.Contains(welcome.HTMLBody, "Welcome, OTP Member.") || !strings.Contains(welcome.TextBody, "Your account is ready") {
		t.Fatal("welcome email did not pass through Identity's mail view and platform delivery")
	}
	// CLI registration borrows the same platform service for metadata reads.
	console.SetOut(io.Discard)
	console.SetArgs([]string{"media", "list", "user", userID.String()})
	if err = console.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
}

type capturedMail struct {
	mu       sync.Mutex
	messages []mail.Message
}

func (s *capturedMail) Send(_ context.Context, m mail.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, m)
	return nil
}

// Welcome delivery is concurrent with authentication; read the latest code email,
// not whichever unrelated message happened to finish last.
func (s *capturedMail) lastCode() mail.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.messages) - 1; i >= 0; i-- {
		if regexp.MustCompile(`\b[0-9]{6}\b`).MatchString(s.messages[i].TextBody) {
			return s.messages[i]
		}
	}
	return mail.Message{}
}

func databaseWelcome() notification.Message {
	content := notifications.NewWelcome("Member")
	content.Email = nil
	return content
}

func (s *capturedMail) welcomeFor(email string) mail.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.messages {
		if m.Subject == "Welcome to your account" && len(m.To) == 1 && m.To[0].Email == email {
			return m
		}
	}
	return mail.Message{}
}

// Decode the public success envelope; fail if the endpoint returned a problem or
// a legacy bare body instead. Business assertions below use the typed result.
func decodeResponseData(data []byte, out any) error {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope.Data, out)
}
