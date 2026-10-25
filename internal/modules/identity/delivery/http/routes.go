// Package http exposes authentication and self-service account, media and notification endpoints.
package http

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/platform/api"
)

// Routes selects Identity's public capabilities. Both APIs are borrowed.
type Routes struct {
	Users         service.UserService
	Auth          service.AuthService
	Media         Assets
	Notifications Notifications
	SignIn        bool
	Social        bool
}

// RegisterRoutes registers each operation once for routing and OpenAPI.
// protected must already enforce bearer authentication. No resources are started.
func RegisterRoutes(public, protected huma.API, routes Routes) {
	if public != nil {
		if routes.Auth != nil && routes.SignIn {
			registerAuth(public, protected, routes.Auth, routes.Social)
		}
	}
	if protected != nil {
		if routes.Users != nil {
			registerAccount(protected, routes.Users)
		}
		if routes.Media != nil {
			registerMedia(protected, routes.Media)
		}
		if routes.Notifications != nil {
			registerNotifications(protected, routes.Notifications)
			registerSubscriptions(protected, routes.Notifications, routes.Auth)
		}
	}
}

func identityAPI(router huma.API) huma.API {
	group := huma.NewGroup(router)
	group.UseSimpleModifier(func(op *huma.Operation) { op.Tags = []string{"identity"} })
	group.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) { ctx.SetHeader("Cache-Control", "no-store"); next(ctx) })
	return group
}

func registerAccount(router huma.API, svc service.UserService) {
	h := newUserHandler(svc)
	routes := identityAPI(router)
	huma.Register(routes, huma.Operation{OperationID: "identity.currentUser", Method: "GET", Path: "/users/me", Summary: "Get the authenticated user"}, h.currentUser)
	huma.Register(routes, huma.Operation{OperationID: "identity.updateProfile", Method: "PATCH", Path: "/users/me", Summary: "Update your profile", MaxBodyBytes: 4096}, h.updateProfile)
	huma.Register(routes, huma.Operation{OperationID: "identity.getUser", Method: "GET", Path: "/users/{id}", Summary: "Get your own user account"}, h.getUser)
}

func registerAuth(public, protected huma.API, svc service.AuthService, social bool) {
	h := &authHandler{svc: svc}
	routes := identityAPI(public)
	signIns := huma.NewGroup(routes)
	signIns.UseSimpleModifier(func(op *huma.Operation) {
		created := *op.Responses["200"]
		created.Description = "New account created"
		op.Responses["201"] = &created
	})

	huma.Register(routes, huma.Operation{OperationID: "identity.requestSignIn", Method: "POST", Path: "/auth/otp/send", Summary: "Send a passwordless sign-in code", DefaultStatus: 202, MaxBodyBytes: 1024}, h.requestSignIn)
	huma.Register(signIns, huma.Operation{OperationID: "identity.confirmSignIn", Method: "POST", Path: "/auth/otp/verify", Summary: "Sign in or create an account with an email code", MaxBodyBytes: 2048}, h.confirmSignIn)
	huma.Register(routes, huma.Operation{OperationID: "identity.refresh", Method: "POST", Path: "/auth/refresh", Summary: "Rotate a refresh token", MaxBodyBytes: 1024}, h.refresh)
	if social {
		huma.Register(signIns, huma.Operation{OperationID: "identity.socialSignIn", Method: "POST", Path: "/auth/social/{provider}", Summary: "Sign in with a provider ID token", MaxBodyBytes: 20 * 1024}, h.socialSignIn)
	}
	if protected == nil {
		return
	}
	routes = identityAPI(protected)
	huma.Register(routes, huma.Operation{OperationID: "identity.logout", Method: "POST", Path: "/auth/logout", Summary: "Revoke the current session", DefaultStatus: 204}, h.logout)
	huma.Register(routes, huma.Operation{OperationID: "identity.list", Method: "GET", Path: "/auth/sessions", Summary: "List your active sessions"}, h.list)
	huma.Register(routes, huma.Operation{OperationID: "identity.revokeAll", Method: "DELETE", Path: "/auth/sessions", Summary: "Revoke all your sessions", DefaultStatus: 204}, h.revokeAll)
	huma.Register(routes, huma.Operation{OperationID: "identity.revoke", Method: "DELETE", Path: "/auth/sessions/{id}", Summary: "Revoke one of your sessions", DefaultStatus: 204}, h.revoke)
}

func registerMedia(router huma.API, svc Assets) {
	h := &mediaHandler{svc: svc}
	routes := huma.NewGroup(router)
	routes.UseSimpleModifier(func(op *huma.Operation) { op.Tags = []string{"media"} })
	huma.Register(routes, huma.Operation{OperationID: "media.upload", Method: "POST", Path: "/media", Summary: "Upload an asset (public by default)", DefaultStatus: 201,
		MaxBodyBytes: uploadLimit + (64 << 10), Middlewares: huma.Middlewares{api.Multipart},
		RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"multipart/form-data": {Schema: &huma.Schema{Type: "object", Required: []string{"file"}, AdditionalProperties: false, Properties: map[string]*huma.Schema{"file": {Type: "string", Format: "binary", Description: "Exactly one file, maximum 20 MiB"}, "visibility": {Type: "string", Enum: []any{"public", "private"}, Description: "Defaults to the selected disk visibility"}}}}}},
	}, h.upload)
	huma.Register(routes, huma.Operation{OperationID: "media.list", Method: "GET", Path: "/media", Summary: "List your assets"}, h.list)
	huma.Register(routes, huma.Operation{OperationID: "media.get", Method: "GET", Path: "/media/{id}", Summary: "Get owned asset metadata"}, h.get)
	huma.Register(routes, huma.Operation{OperationID: "media.setVisibility", Method: "PATCH", Path: "/media/{id}/visibility", Summary: "Change an owned asset's visibility"}, h.setVisibility)
	huma.Register(routes, huma.Operation{OperationID: "media.download", Method: "GET", Path: "/media/{id}/download", Summary: "Download an owned ready asset",
		Responses: map[string]*huma.Response{"200": {Description: "Private attachment", Content: map[string]*huma.MediaType{"application/octet-stream": {Schema: &huma.Schema{Type: "string", Format: "binary"}}}}},
	}, h.download)
	huma.Register(routes, huma.Operation{OperationID: "media.temporary-url", Method: "POST", Path: "/media/{id}/url", Summary: "Create an expiring asset link"}, h.temporaryURL)
	huma.Register(routes, huma.Operation{OperationID: "media.delete", Method: "DELETE", Path: "/media/{id}", Summary: "Delete an owned asset", DefaultStatus: 204}, h.delete)
}

// registerNotifications borrows an authenticated router. Handlers resolve the same
// platform principal used by every module and fail closed when it is absent.
func registerNotifications(router huma.API, notifier Notifications) {
	h := &notificationHandler{svc: notifier}
	routes := notificationAPI(router)
	huma.Register(routes, huma.Operation{OperationID: "notifications.list", Method: "GET", Path: "/notifications", Summary: "List your notifications"}, h.list)
	huma.Register(routes, huma.Operation{OperationID: "notifications.unreadCount", Method: "GET", Path: "/notifications/unread-count", Summary: "Count your unread notifications"}, h.unreadCount)
	huma.Register(routes, huma.Operation{OperationID: "notifications.markRead", Method: "PATCH", Path: "/notifications/{id}/read", Summary: "Mark a notification as read", DefaultStatus: 204}, h.markRead)
}

func notificationAPI(router huma.API) huma.API {
	group := huma.NewGroup(router)
	group.UseSimpleModifier(func(op *huma.Operation) { op.Tags = []string{"notifications"} })
	group.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) { ctx.SetHeader("Cache-Control", "no-store"); next(ctx) })
	return group
}

// registerSubscriptions uses Identity's session-locked registration operation.
// A missing authentication service fails closed even on a misconfigured router.
func registerSubscriptions(router huma.API, notifications Notifications, authentication service.AuthService) {
	h := &subscriptionHandler{notifications: notifications, auth: authentication}
	routes := notificationAPI(router)
	huma.Register(routes, huma.Operation{OperationID: "notifications.subscribe", Method: "POST", Path: "/notifications/subscriptions", Summary: "Register or refresh this installation for push", MaxBodyBytes: 8192}, h.subscribe)
	huma.Register(routes, huma.Operation{OperationID: "notifications.unsubscribe", Method: "DELETE", Path: "/notifications/subscriptions/{id}", Summary: "Detach your push installation", DefaultStatus: 204}, h.unsubscribe)
}
