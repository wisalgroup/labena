package modules_test

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/modules/identity"
	identitydomain "github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/internal/platform/dependencies"
	"github.com/wisalgroup/labena/internal/platform/media"
	mediaintegration "github.com/wisalgroup/labena/internal/platform/media/integration"
	"github.com/wisalgroup/labena/internal/platform/notification"
	notificationintegration "github.com/wisalgroup/labena/internal/platform/notification/integration"
	asynqmiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
)

func TestModuleRegistrationValidatesBeforeMutation(t *testing.T) {
	notifier, err := notification.New(nil, notification.Options{})
	if err != nil {
		t.Fatal(err)
	}
	registerIdentity := func(targets dependencies.Targets) error {
		return identity.Register(dependencies.Resources{Notifier: notifier}, targets, nil, false)
	}

	for _, tc := range []struct {
		name     string
		register func(dependencies.Targets) error
		want     error
	}{
		{"identity subscriber", registerIdentity, identitydomain.ErrSubscriberRequired},
		{"identity queue", func(targets dependencies.Targets) error {
			bus := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})
			defer bus.Close()
			targets.EventSubscriber = bus
			return registerIdentity(targets)
		}, identitydomain.ErrQueueRequired},
		{"media unconstructed", func(t dependencies.Targets) error { return mediaintegration.Register(nil, t) }, media.ErrLibraryRequired},
		{"notification unconstructed", func(t dependencies.Targets) error { return notificationintegration.Register(nil, t) }, notification.ErrNotifierRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := newAPI(t)
			events, err := message.NewRouter(message.RouterConfig{}, watermill.NopLogger{})
			if err != nil {
				t.Fatal(err)
			}
			commands, err := cli.New(cli.Config{})
			if err != nil {
				t.Fatal(err)
			}
			targets := dependencies.Targets{RootCommand: commands, HTTP: router, AuthenticatedHTTP: router, EventRouter: events, TaskRouter: asynq.NewServeMux()}
			if err := tc.register(targets); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if _, pattern := targets.TaskRouter.Handler(asynq.NewTask(notification.DeliveryTask, nil)); pattern != "" {
				t.Fatal("failed registration added a task handler")
			}
			if len(router.OpenAPI().Paths) != 0 || len(events.Handlers()) != 0 || len(commands.Commands()) != 0 {
				t.Fatal("failed registration mutated targets")
			}
		})
	}
}

func TestIdentityRejectsGoogleWithoutAuthentication(t *testing.T) {
	router := newAPI(t)
	commands, err := cli.New(cli.Config{})
	if err != nil {
		t.Fatal(err)
	}
	targets := dependencies.Targets{HTTP: router, AuthenticatedHTTP: router, RootCommand: commands}
	if err := identity.Register(dependencies.Resources{}, targets, nil, true); err == nil {
		t.Fatal("Google routes accepted without an authentication service")
	}
	if len(router.OpenAPI().Paths) != 0 || len(commands.Commands()) != 0 {
		t.Fatal("invalid authentication selection mutated targets")
	}
}

func TestIdentityOwnsUserCapabilityHTTP(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		capabilities, protected bool
	}{
		{"available", true, true},
		{"omitted capabilities", false, true},
		{"omitted protected target", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := newAPI(t)
			targets := dependencies.Targets{HTTP: router}
			if tc.protected {
				targets.AuthenticatedHTTP = api.Authenticated(router, nil)
			}
			resources := dependencies.Resources{}
			if tc.capabilities {
				var err error
				resources.Media, err = media.New(nil, nil, "", media.Config{})
				if err != nil {
					t.Fatal(err)
				}
				resources.Notifier, err = notification.New(nil, notification.Options{})
				if err != nil {
					t.Fatal(err)
				}
				if err := mediaintegration.Register(resources.Media, targets); err != nil {
					t.Fatal(err)
				}
				if err := notificationintegration.Register(resources.Notifier, targets); err != nil {
					t.Fatal(err)
				}
				if len(router.OpenAPI().Paths) != 1 || router.OpenAPI().Paths["/media/{id}/content"] == nil {
					t.Fatal("platform registration must expose only generic content serving")
				}
			}
			if err := identity.Register(resources, targets, nil, false); err != nil {
				t.Fatal(err)
			}
			for _, route := range []struct{ method, path string }{
				{"GET", "/media"}, {"GET", "/notifications"}, {"POST", "/notifications/subscriptions"},
			} {
				want := 404
				if tc.capabilities && tc.protected {
					want = 401
				}
				out := httptest.NewRecorder()
				request := httptest.NewRequest(route.method, route.path, nil)
				request.Header.Set("Authorization", "Bearer forged")
				router.ServeHTTP(out, request)
				if out.Code != want {
					t.Fatalf("%s %s: got %d want %d: %s", route.method, route.path, out.Code, want, out.Body.String())
				}
			}
		})
	}
}

func TestModulesRegisterOnlySelectedTransports(t *testing.T) {
	queue := asynqmiddleware.EnqueueFunc(func(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
		t.Fatal("registration enqueued work")
		return nil, nil
	})
	notifier, err := notification.New(nil, notification.Options{Queue: queue})
	if err != nil {
		t.Fatal(err)
	}
	uploads, err := media.New(nil, nil, "", media.Config{})
	if err != nil {
		t.Fatal(err)
	}
	bus := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})
	defer bus.Close()
	for _, selection := range []string{"none", "commands", "http", "tasks", "events"} {
		t.Run(selection, func(t *testing.T) {
			targets := dependencies.Targets{}
			http := newAPI(t)
			router, err := message.NewRouter(message.RouterConfig{}, watermill.NopLogger{})
			if err != nil {
				t.Fatal(err)
			}
			switch selection {
			case "commands":
				targets.RootCommand, err = cli.New(cli.Config{})
				if err != nil {
					t.Fatal(err)
				}
			case "http":
				targets.HTTP = http
				targets.AuthenticatedHTTP = http
			case "tasks":
				targets.TaskRouter = asynq.NewServeMux()
			case "events":
				targets.EventRouter, targets.EventSubscriber = router, bus
			}
			if err := notificationintegration.Register(notifier, targets); err != nil {
				t.Fatal(err)
			}
			if err := mediaintegration.Register(uploads, targets); err != nil {
				t.Fatal(err)
			}
			if selection == "commands" {
				if err := cli.Validate(targets.RootCommand); err != nil {
					t.Fatal(err)
				}
				if len(targets.RootCommand.Commands()) != 2 {
					t.Fatal("module commands missing")
				}
			}
			if selection == "http" && (len(http.OpenAPI().Paths) != 1 || http.OpenAPI().Paths["/media/{id}/content"] == nil) {
				t.Fatal("platform must register only generic media content HTTP")
			}
			if selection == "tasks" {
				if _, pattern := targets.TaskRouter.Handler(asynq.NewTask(notification.DeliveryTask, nil)); pattern == "" {
					t.Fatal("task handler missing")
				}
			}
			if selection == "events" && len(router.Handlers()) != 0 {
				t.Fatal("platform capabilities must not own business events")
			}
		})
	}
}
func TestModuleCommandsUseBorrowedServices(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000001"
	failure := errors.New("enqueue failed")
	for _, tc := range []struct {
		args      []string
		enqueue   bool
		wantError bool
	}{
		{[]string{"notifications", "--help"}, false, false},
		{[]string{"identity", "welcome"}, false, true},
		{[]string{"identity", "welcome", "bad"}, false, true},
		{[]string{"identity", "welcome", id}, true, true},
		{[]string{"media", "list", "bad"}, false, true},
		{[]string{"identity", "show", "bad"}, false, true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			resources := dependencies.Resources{Queue: asynqmiddleware.EnqueueFunc(func(got context.Context, _ *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
				calls++
				if got != ctx {
					t.Fatal("command lost caller context")
				}
				return nil, failure
			})}
			notifier, err := notification.New(resources.DB, notification.Options{Queue: resources.Queue, Mailer: resources.Mailer, MailRenderer: resources.MailRenderer})
			if err != nil {
				t.Fatal(err)
			}
			uploads, err := media.New(resources.DB, resources.Disks, resources.DefaultDisk, media.Config{})
			if err != nil {
				t.Fatal(err)
			}
			resources.Notifier = notifier
			registerIdentity := func(targets dependencies.Targets) error {
				return identity.Register(resources, targets, nil, false)
			}
			root, err := cli.New(cli.Config{Out: io.Discard, Err: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			targets := dependencies.Targets{RootCommand: root}
			for _, register := range []func(dependencies.Targets) error{registerIdentity, func(t dependencies.Targets) error { return notificationintegration.Register(notifier, t) }, func(t dependencies.Targets) error { return mediaintegration.Register(uploads, t) }} {
				if err := register(targets); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 0 {
				t.Fatal("construction or registration performed I/O")
			}
			if err := cli.Validate(root); err != nil {
				t.Fatal(err)
			}
			root.SetArgs(tc.args)
			err = root.ExecuteContext(ctx)
			if (err != nil) != tc.wantError {
				t.Fatal(err)
			}
			if tc.enqueue {
				if calls != 1 || !errors.Is(err, failure) {
					t.Fatal(calls, err)
				}
			} else if calls != 0 {
				t.Fatal("invalid input or help enqueued work")
			}
		})
	}
}

func TestPlatformTasksRejectInvalidDeliveryBeforePersistence(t *testing.T) {
	m, err := notification.New(nil, notification.Options{})
	if err != nil {
		t.Fatal(err)
	}
	mux := asynq.NewServeMux()
	if err := notificationintegration.Register(m, dependencies.Targets{TaskRouter: mux}); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`{}`, `null`, `"00000000-0000-0000-0000-000000000000"`, `broken`} {
		if err := mux.ProcessTask(t.Context(), asynq.NewTask(notification.DeliveryTask, []byte(data))); !errors.Is(err, asynq.SkipRetry) {
			t.Fatal("invalid payload was retryable", data, err)
		}
	}
}

func newAPI(t testing.TB, options ...api.Options) *api.Server {
	t.Helper()
	var opts api.Options
	if len(options) > 0 {
		opts = options[0]
	}
	server, err := api.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}
