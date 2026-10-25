package http

import (
	"mime/multipart"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/media"
)

type GetUserRequest struct {
	ID string `path:"id" format:"uuid"`
}

type SignInRequest struct {
	Body struct {
		Name        string `json:"name" maxLength:"100" required:"false"`
		ChallengeID string `json:"challenge_id" format:"uuid"`
		// Code is exactly six decimal digits, including any leading zero.
		Code       string                `json:"code" minLength:"6" maxLength:"6" pattern:"^[0-9]{6}$"`
		Platform   enums.SessionPlatform `json:"platform" enum:"ios,android,web"`
		DeviceName string                `json:"device_name" maxLength:"100" required:"false"`
	}
}

func (req *SignInRequest) Input(ip string) service.SignInInput {
	return service.SignInInput{
		Name:   req.Body.Name,
		Proof:  service.ProofInput{ChallengeID: uuid.MustParse(req.Body.ChallengeID), Code: req.Body.Code, ClientIP: ip},
		Device: service.Device{Platform: req.Body.Platform, Name: req.Body.DeviceName},
	}
}

type RefreshRequest struct {
	Body struct {
		// Token is the opaque 43-character refresh credential, not an access token.
		Token string `json:"refresh_token" minLength:"43" maxLength:"43"`
	}
}
type SessionRequest struct {
	ID string `path:"id" format:"uuid"`
}

type SignInCodeRequest struct {
	Body struct {
		Email string `json:"email" format:"email" minLength:"1" maxLength:"254"`
	}
}

// Provider availability is enforced by the service; unsupported names fail closed.
type SocialSignInRequest struct {
	Provider enums.AuthProvider `path:"provider"`
	Body     struct {
		IDToken    string                `json:"id_token" minLength:"1" maxLength:"16384"`
		Platform   enums.SessionPlatform `json:"platform" enum:"ios,android,web"`
		DeviceName string                `json:"device_name" maxLength:"100" required:"false"`
	}
}

// UpdateProfileRequest deliberately excludes email, status and identifiers.
type UpdateProfileRequest struct {
	Body struct {
		Name *string `json:"name" maxLength:"100" nullable:"true" required:"false"`
	}
}

func (req *UpdateProfileRequest) Input() service.UpdateProfileInput {
	return service.UpdateProfileInput{Name: req.Body.Name}
}

type TemporaryURLRequest struct {
	AssetRequest
	Body struct {
		ExpiresIn int `json:"expires_in,omitempty" minimum:"1" maximum:"86400" default:"900" doc:"Link lifetime in seconds"`
	}
}

type SetAssetVisibilityRequest struct {
	AssetRequest
	Body struct {
		Visibility media.Visibility `json:"visibility" enum:"public,private"`
	}
}

type UploadRequest struct{ RawBody multipart.Form }

type ListAssetsRequest struct {
	api.PaginationInput
	Collection string `query:"collection"`
}

type AssetRequest struct {
	ID string `path:"id" format:"uuid"`
}

type NotificationRequest struct {
	ID string `path:"id" format:"uuid"`
}

type SubscriptionRequest struct {
	InstallationID string `json:"installation_id" format:"uuid"`
	Registration   string `json:"registration" minLength:"1" maxLength:"4096"`
}

// SubscribeRequest wraps the installation registration body.
type SubscribeRequest struct{ Body SubscriptionRequest }
