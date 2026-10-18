package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	identityhttp "github.com/wisalgroup/labena/internal/modules/identity/delivery/http"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/auth"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/pagination"
)

func TestPushRegistrationUsesIdentityService(t *testing.T) {
	userID, installationID, subscriptionID := uuid.New(), uuid.NewString(), uuid.New()
	notifier, err := notification.New(nil, notification.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body    string
		authorized    bool
		missingAuth   bool
		callbackErr   error
		status, calls int
	}{
		{name: "session callback", authorized: true, status: 200, calls: 1},
		{name: "session denied", authorized: true, callbackErr: apperror.Unauthenticated("Session revoked."), status: 401, calls: 1},
		{name: "missing bearer", status: 401},
		{name: "invalid input", authorized: true, body: `{"installation_id":"invalid","registration":"ExpoPushToken[test]"}`, status: 422},
		{name: "missing authentication fails closed", authorized: true, missingAuth: true, status: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var authentication service.AuthService
			if !tc.missingAuth {
				authentication = pushHTTPAuth{subscribe: func(ctx context.Context, principal auth.Principal, installation, registration string) (*notification.Subscription, error) {
					calls++
					if ctx.Err() != nil || principal.UserID != userID || principal.SessionID == uuid.Nil || installation != installationID || registration != "ExpoPushToken[test]" {
						t.Fatal("callback lost authenticated identity or input")
					}
					return &notification.Subscription{ID: subscriptionID, InstallationID: installationID, UpdatedAt: time.Now()}, tc.callbackErr
				}}
			}
			router := newCapabilityAPI(t)

			identityhttp.RegisterRoutes(nil, api.Authenticated(router, recordAuth{userID}), identityhttp.Routes{Notifications: notifier, Auth: authentication})
			body := tc.body
			if body == "" {
				body = `{"installation_id":"` + installationID + `","registration":"ExpoPushToken[test]"}`
			}
			request := httptest.NewRequest("POST", "/notifications/subscriptions", strings.NewReader(body))
			if tc.authorized {
				request.Header.Set("Authorization", "Bearer test")
			}
			out := httptest.NewRecorder()
			router.ServeHTTP(out, request)
			if out.Code != tc.status || calls != tc.calls {
				t.Fatal(out.Code, out.Body.String(), "callback calls", calls)
			}
			if tc.status == 200 && !strings.Contains(out.Body.String(), subscriptionID.String()) {
				t.Fatal("callback result not returned", out.Body.String())
			}
		})
	}
}

type notificationService struct {
	calls int
	page  pagination.Pagination
	err   error
}

func (s *notificationService) List(_ context.Context, _ notification.Recipient, p pagination.Pagination) (*pagination.Page[*notification.Record], error) {
	s.calls++
	s.page = p
	return pagination.NewPage([]*notification.Record{}, p, 0), s.err
}
func TestNotificationHTTP(t *testing.T) {
	id := uuid.New()
	for _, tc := range []struct {
		path   string
		err    error
		status int
	}{
		{"?page=0", nil, 422}, {"?page=2&per_page=3", nil, 200}, {"", errors.New("private SQL detail"), 500},
	} {
		t.Run(tc.path, func(t *testing.T) {
			notifier := &notificationService{err: tc.err}
			router := newCapabilityAPI(t)

			identityhttp.RegisterRoutes(nil, api.Authenticated(router, recordAuth{id}), identityhttp.Routes{Notifications: notifier})
			out := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/notifications"+tc.path, nil)
			req.Header.Set("Authorization", "Bearer test")
			router.ServeHTTP(out, req)
			if out.Code != tc.status || strings.Contains(out.Body.String(), "private SQL") {
				t.Fatal(out.Code, out.Body.String())
			}
			if tc.status == 422 && notifier.calls != 0 {
				t.Fatal("invalid input reached service")
			}
			if tc.status == 200 {
				var body response.Envelope[pagination.Page[identityhttp.NotificationResponse]]
				if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Data.Items == nil || body.Data.Page != 2 || body.Data.PerPage != 3 || notifier.page.PerPage != 3 {
					t.Fatal(out.Body.String())
				}
			}
		})
	}
}

func TestNotificationRoutesRequireTrustedRecipient(t *testing.T) {
	router := newCapabilityAPI(t)

	notifier := &notificationService{}
	identityhttp.RegisterRoutes(nil, router, identityhttp.Routes{Notifications: notifier})
	for _, route := range []struct{ method, path string }{
		{"GET", "/notifications"}, {"GET", "/notifications/unread-count"}, {"PATCH", "/notifications/" + uuid.NewString() + "/read"},
		{"DELETE", "/notifications/subscriptions/" + uuid.NewString()},
	} {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Header.Set("X-User-ID", uuid.NewString())
		req.Header.Set("Authorization", "Bearer forged")
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != 401 || notifier.calls != 0 || out.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(out.Code, out.Body.String())
		}
	}
}

type scopedRecords struct {
	userID         notification.Recipient
	notificationID uuid.UUID
}

func (s *scopedRecords) UnreadCount(_ context.Context, id notification.Recipient) (int, error) {
	s.userID = id
	return 3, nil
}
func (s *scopedRecords) MarkRead(_ context.Context, userID notification.Recipient, id uuid.UUID) error {
	s.userID = userID
	s.notificationID = id
	return nil
}
func TestNotificationReadEndpointsUseAuthenticatedRecipient(t *testing.T) {
	notifier := &scopedRecords{}
	userID := uuid.New()
	notificationID := uuid.New()
	router := newCapabilityAPI(t)

	identityhttp.RegisterRoutes(nil, api.Authenticated(router, recordAuth{userID}), identityhttp.Routes{Notifications: notifier})
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/notifications/unread-count?user_id=" + uuid.NewString(), 200},
		{"PATCH", "/notifications/" + notificationID.String() + "/read", 204},
		{"DELETE", "/notifications/subscriptions/" + notificationID.String() + "?user_id=" + uuid.NewString(), 204},
	} {
		out := httptest.NewRecorder()
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Header.Set("Authorization", "Bearer test")
		router.ServeHTTP(out, req)
		if out.Code != route.status || notifier.userID != (notification.Recipient{Type: "user", ID: userID.String()}) {
			t.Fatal(out.Code, out.Body.String())
		}
	}
	if notifier.notificationID != notificationID {
		t.Fatal("wrong notification")
	}
}

func (s *notificationService) UnreadCount(context.Context, notification.Recipient) (int, error) {
	s.calls++
	return 0, s.err
}
func (s *notificationService) MarkRead(context.Context, notification.Recipient, uuid.UUID) error {
	s.calls++
	return s.err
}
func (s *scopedRecords) List(_ context.Context, id notification.Recipient, p pagination.Pagination) (*pagination.Page[*notification.Record], error) {
	s.userID = id
	return pagination.NewPage([]*notification.Record{}, p, 0), nil
}

type recordAuth struct{ id uuid.UUID }

func (a recordAuth) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{UserID: a.id, SessionID: uuid.New()}, nil
}

type jsonRecords struct{ notificationService }

func (s *jsonRecords) List(_ context.Context, recipient notification.Recipient, p pagination.Pagination) (*pagination.Page[*notification.Record], error) {
	return pagination.NewPage([]*notification.Record{{ID: uuid.New(), Recipient: recipient, Type: "order.shipped", Data: json.RawMessage(`{"order_id":9007199254740993,"action":{"url":"/orders/42"},"items":[1,true]}`)}}, p, 1), nil
}

func TestNotificationHTTPReturnsJSONObject(t *testing.T) {
	router := newCapabilityAPI(t)
	identityhttp.RegisterRoutes(nil, api.Authenticated(router, recordAuth{uuid.New()}), identityhttp.Routes{Notifications: &jsonRecords{}})
	request := httptest.NewRequest("GET", "/notifications", nil)
	request.Header.Set("Authorization", "Bearer test")
	out := httptest.NewRecorder()
	router.ServeHTTP(out, request)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	var result struct {
		Data struct {
			Items []struct {
				Type string                     `json:"type"`
				Data map[string]json.RawMessage `json:"data"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data.Items) != 1 || result.Data.Items[0].Type != "order.shipped" || string(result.Data.Items[0].Data["order_id"]) != "9007199254740993" {
		t.Fatal(out.Body.String())
	}
}

type pushHTTPAuth struct {
	service.AuthService
	subscribe func(context.Context, auth.Principal, string, string) (*notification.Subscription, error)
}

func (a pushHTTPAuth) SubscribePush(ctx context.Context, principal auth.Principal, installationID, registration string) (*notification.Subscription, error) {
	return a.subscribe(ctx, principal, installationID, registration)
}

func (s *notificationService) Unsubscribe(context.Context, notification.Recipient, uuid.UUID) error {
	s.calls++
	return s.err
}

func (s *scopedRecords) Unsubscribe(_ context.Context, recipient notification.Recipient, id uuid.UUID) error {
	s.userID, s.notificationID = recipient, id
	return nil
}
