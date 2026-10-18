package http

import (
	"context"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/api/response"
)

type authHandler struct{ svc service.AuthService }

func (h *authHandler) requestSignIn(ctx context.Context, req *SignInCodeRequest) (*response.JSON[ChallengeResponse], error) {
	ip, err := verificationClientIP(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	id, err := h.svc.RequestSignInCode(ctx, req.Body.Email, ip)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.Accepted(newChallengeResponse(id)), nil
}

func (h *authHandler) confirmSignIn(ctx context.Context, req *SignInRequest) (*response.JSON[SignInResponse], error) {
	ip, err := verificationClientIP(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	result, err := h.svc.SignInWithCode(ctx, req.Input(ip))
	return respondSignIn(ctx, result, err)
}

func (h *authHandler) refresh(ctx context.Context, req *RefreshRequest) (*response.JSON[TokensResponse], error) {
	tokens, err := h.svc.Refresh(ctx, req.Body.Token)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.OK(newTokensResponse(tokens)), nil
}

func (h *authHandler) list(ctx context.Context, req *api.PaginationInput) (*response.JSON[*SessionPage], error) {
	principal, err := api.Principal(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	page := req.Pagination()
	result, err := h.svc.ListSessions(ctx, principal.UserID, page)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.OK(sessionPage(result, principal.SessionID)), nil
}

func (h *authHandler) revokeAll(ctx context.Context, in *struct{}) (*response.Empty, error) {
	userID, err := api.CurrentUserID(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	if err := h.svc.RevokeSessions(ctx, userID); err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.NoContent(), nil
}

func (h *authHandler) revoke(ctx context.Context, req *SessionRequest) (*response.Empty, error) {
	userID, err := api.CurrentUserID(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	if err := h.svc.RevokeSession(ctx, userID, uuid.MustParse(req.ID)); err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.NoContent(), nil
}

func (h *authHandler) logout(ctx context.Context, in *struct{}) (*response.Empty, error) {
	principal, err := api.Principal(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	if err := h.svc.RevokeSession(ctx, principal.UserID, principal.SessionID); err != nil {
		return nil, response.Error(ctx, err)
	}
	return response.NoContent(), nil
}

func respondSignIn(ctx context.Context, result *service.SignInResult, err error) (*response.JSON[SignInResponse], error) {
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	status := 200
	if result.Created {
		status = 201
	}
	return response.NewJSON(newSignInResponse(result.Tokens, result.User), status), nil
}

func (h *authHandler) socialSignIn(ctx context.Context, req *SocialSignInRequest) (*response.JSON[SignInResponse], error) {
	ip, err := verificationClientIP(ctx)
	if err != nil {
		return nil, response.Error(ctx, err)
	}
	result, err := h.svc.SignInWithProvider(
		ctx,
		req.Provider,
		req.Body.IDToken,
		service.SocialSignInInput{
			ClientIP: ip,
			Device:   service.Device{Platform: req.Body.Platform, Name: req.Body.DeviceName},
		},
	)
	return respondSignIn(ctx, result, err)
}

// Use the immediate peer until a trusted ingress policy exists. Never accept
// arbitrary forwarding headers as rate-limit identity. NAT/proxies share a budget.
func verificationClientIP(ctx context.Context) (string, error) {
	ip, err := api.ClientIP(ctx)
	if err != nil {
		return "", domain.ErrVerificationInvalid
	}
	return ip, nil
}
