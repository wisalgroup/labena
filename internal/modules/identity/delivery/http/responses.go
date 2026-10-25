package http

import (
	"encoding/json"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/media"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/pagination"
)

// UserResponse is the public account representation for sign-in and profile operations.
type UserResponse struct {
	ID              uuid.UUID        `json:"id" format:"uuid"`
	Email           string           `json:"email"`
	EmailVerifiedAt *time.Time       `json:"email_verified_at" nullable:"true" format:"date-time"`
	Phone           *string          `json:"phone" nullable:"true"`
	PhoneVerifiedAt *time.Time       `json:"phone_verified_at" nullable:"true" format:"date-time"`
	Status          enums.UserStatus `json:"status" enum:"active,suspended,pending_deletion,deleted"`
	ProfileComplete bool             `json:"profile_complete" doc:"Whether all required profile fields have been supplied."`
	Name            string           `json:"name"`
	CreatedAt       time.Time        `json:"created_at" format:"date-time"`
	UpdatedAt       time.Time        `json:"updated_at" format:"date-time"`
}

func newUserResponse(user *entities.User) UserResponse {
	return UserResponse{
		ID:    user.ID,
		Email: user.Email.String(), EmailVerifiedAt: user.EmailVerifiedAt,
		Phone: user.Phone, PhoneVerifiedAt: user.PhoneVerifiedAt,
		Status: user.Status, Name: user.Name, ProfileComplete: user.ProfileComplete(),
		CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

type SessionResponse struct {
	ID         uuid.UUID             `json:"id" format:"uuid"`
	Platform   enums.SessionPlatform `json:"platform" enum:"ios,android,web"`
	DeviceName *string               `json:"device_name" nullable:"true"`
	CreatedAt  time.Time             `json:"created_at" format:"date-time"`
	LastUsedAt time.Time             `json:"last_used_at" format:"date-time"`
	ExpiresAt  *time.Time            `json:"expires_at" nullable:"true" format:"date-time"` // Null means no absolute session deadline.
	Current    bool                  `json:"current"`
}

// SessionPage is the paginated session response.
type SessionPage = pagination.Page[SessionResponse]

func sessionPage(page *pagination.Page[*entities.Session], current uuid.UUID) *SessionPage {
	items := make([]SessionResponse, len(page.Items))
	for i, s := range page.Items {
		items[i] = SessionResponse{s.ID, s.Platform, s.DeviceName, s.CreatedAt, s.LastUsedAt, s.ExpiresAt, s.ID == current}
	}
	return pagination.NewPage(items, pagination.NewPagination(page.Page, page.PerPage), page.Total)
}

type ChallengeResponse struct {
	ChallengeID uuid.UUID `json:"challenge_id" format:"uuid"`
	Message     string    `json:"message"`
}

func newChallengeResponse(challengeID uuid.UUID) ChallengeResponse {
	return ChallengeResponse{ChallengeID: challengeID, Message: "Check your email for a sign-in code."}
}

// SignInResponse includes the account for both new and returning users.
// Check User.ProfileComplete after either a 200 or 201 response.
type SignInResponse struct {
	TokensResponse
	User UserResponse `json:"user"`
}

func newSignInResponse(tokens *service.Tokens, user *entities.User) SignInResponse {
	return SignInResponse{TokensResponse: newTokensResponse(tokens), User: newUserResponse(user)}
}

type TokensResponse struct {
	AccessToken      string     `json:"access_token"`
	RefreshToken     string     `json:"refresh_token"`
	TokenType        string     `json:"token_type"`
	ExpiresIn        int64      `json:"expires_in"`
	RefreshExpiresAt *time.Time `json:"refresh_expires_at" nullable:"true" format:"date-time"` // Null means no time-based expiry.
	SessionID        uuid.UUID  `json:"session_id" format:"uuid"`
}

func newTokensResponse(tokens *service.Tokens) TokensResponse {
	return TokensResponse{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, TokenType: tokens.TokenType, ExpiresIn: tokens.ExpiresIn, RefreshExpiresAt: tokens.RefreshExpiresAt, SessionID: tokens.SessionID}
}

type AssetResponse struct {
	URLExpiresAt time.Time         `json:"url_expires_at,omitzero"`
	URL          string            `json:"url"`
	Visibility   media.Visibility  `json:"visibility" enum:"public,private"`
	ID           uuid.UUID         `json:"id" format:"uuid"`
	Collection   string            `json:"collection"`
	Filename     string            `json:"filename"`
	ContentType  string            `json:"content_type"`
	Size         int64             `json:"size"`
	Status       media.AssetStatus `json:"status" enum:"pending,ready,changing_visibility,deleting,deleted"`
	CreatedAt    time.Time         `json:"created_at" format:"date-time"`
}

func newAssetResponse(asset *media.Asset, link *media.Link) AssetResponse {
	return AssetResponse{URL: link.URL, URLExpiresAt: link.ExpiresAt, Visibility: asset.Visibility, ID: asset.ID, Collection: asset.Collection, Filename: asset.Filename, ContentType: asset.ContentType, Size: asset.Size, Status: asset.Status, CreatedAt: asset.CreatedAt}
}

type NotificationResponse struct {
	ID            uuid.UUID       `json:"id" format:"uuid"`
	RecipientType string          `json:"recipient_type"`
	RecipientID   string          `json:"recipient_id"`
	Type          string          `json:"type"`
	Data          json.RawMessage `json:"data"`
	ReadAt        *time.Time      `json:"read_at" nullable:"true" format:"date-time"`
	CreatedAt     time.Time       `json:"created_at" format:"date-time"`
	UpdatedAt     time.Time       `json:"updated_at" format:"date-time"`
}

func newNotificationResponse(n *notification.Record) NotificationResponse {
	return NotificationResponse{ID: n.ID, RecipientType: n.Recipient.Type, RecipientID: n.Recipient.ID, Type: n.Type, Data: n.Data, ReadAt: n.ReadAt, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt}
}

// UnreadCountResponse reports unread notifications for the authenticated user.
type UnreadCountResponse struct {
	Count int `json:"count"`
}

// Database notification payloads are JSON objects, not base64-encoded bytes.
func (NotificationResponse) TransformSchema(_ huma.Registry, schema *huma.Schema) *huma.Schema {
	schema.Properties["data"] = &huma.Schema{Type: "object", AdditionalProperties: true}
	return schema
}

type SubscriptionResponse struct {
	ID             uuid.UUID `json:"id" format:"uuid"`
	InstallationID string    `json:"installation_id" format:"uuid"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func newSubscriptionResponse(subscription *notification.Subscription) SubscriptionResponse {
	return SubscriptionResponse{ID: subscription.ID, InstallationID: subscription.InstallationID, UpdatedAt: subscription.UpdatedAt}
}
