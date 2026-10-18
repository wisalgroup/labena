package service

import (
	"context"
	"encoding/hex"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/mailviews"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/modules/shared/enums"
	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/pkg/otp"
	"github.com/wisalgroup/labena/pkg/ratelimit"
)

func (s *authService) requestCode(ctx context.Context, email, clientIP string, purpose enums.VerificationPurpose) (uuid.UUID, error) {
	address, err := valueobjects.NewEmail(email)
	if err != nil {
		return uuid.Nil, entities.ErrInvalidEmail
	}
	client, err := netip.ParseAddr(clientIP)
	if err != nil {
		return uuid.Nil, domain.ErrVerificationInvalid
	}
	if err := s.limit(ctx, "otp-requests", s.digester.Key("destination", address.String()), s.digester.Key("client", client.Unmap().String()), s.rateLimitPolicy.CodeRequestsPerDestination, s.rateLimitPolicy.CodeRequestsPerIP); err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	code, err := otp.Generate()
	if err != nil {
		return uuid.Nil, err
	}
	now := time.Now().UTC()
	c := &entities.VerificationChallenge{ID: id, Purpose: purpose, Channel: enums.ChannelEmail, Destination: address.String(), MaxAttempts: s.verificationCodePolicy.MaxAttemptsPerCode, CreatedAt: now, ExpiresAt: now.Add(s.verificationCodePolicy.CodeLifetime)}
	c.CodeDigest = s.digest(c, code)
	if err = s.verification.IssueEmailChallenge(ctx, c, s.verificationCodePolicy); err != nil {
		return uuid.Nil, err
	}
	deliveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = s.sendCode(deliveryCtx, c.Destination, code, s.verificationCodePolicy.CodeLifetime)
	if err != nil {
		return uuid.Nil, domain.ErrVerificationDelivery
	}
	return id, nil
}

func (s *authService) proof(ctx context.Context, id uuid.UUID, code, clientIP string, purpose enums.VerificationPurpose) (repository.ChallengeProof, error) {
	client, err := netip.ParseAddr(clientIP)
	if err != nil {
		return repository.ChallengeProof{}, domain.ErrVerificationInvalid
	}
	if len(code) > 64 {
		code = ""
	}
	var digest, destination []byte
	c, err := s.verification.GetChallenge(ctx, id)
	if err == nil {
		digest = s.digest(c, code)
		destination = s.digester.Key("destination", c.Destination)
	} else if !errors.Is(err, domain.ErrVerificationInvalid) {
		return repository.ChallengeProof{}, err
	}
	if err := s.limit(ctx, "otp-attempts", destination, s.digester.Key("client", client.Unmap().String()), s.rateLimitPolicy.AttemptsPerIdentity, s.rateLimitPolicy.AttemptsPerIP); err != nil {
		return repository.ChallengeProof{}, err
	}
	return repository.ChallengeProof{ID: id, Digest: digest, Destination: destination, Policy: s.verificationCodePolicy, Purpose: purpose}, nil
}

func (s *authService) PruneVerification(ctx context.Context, limit int) (int64, error) {
	return s.verification.PruneVerification(ctx, limit)
}

func (s *authService) digest(c *entities.VerificationChallenge, code string) []byte {
	return s.digester.Digest(c.ID.String(), string(c.Purpose), string(c.Channel), c.Destination, code)
}

// Budgets are consumed outside SQL transactions. Later failures cannot refund
// attempts. Independent peer/account checks may conservatively charge one bucket
// before another denies the operation; they never fail open.
func (s *authService) limit(ctx context.Context, scope string, destination, client []byte, destinationLimit, clientLimit int) error {
	for _, bucket := range []struct {
		key  []byte
		max  int
		kind string
	}{{client, clientLimit, "peer"}, {destination, destinationLimit, "account"}} {
		if len(bucket.key) == 0 {
			continue
		}
		decision, err := s.limiter.Allow(ctx, "identity:"+scope+":"+bucket.kind+":"+hex.EncodeToString(bucket.key), ratelimit.Limit{Rate: bucket.max, Burst: bucket.max, Period: s.rateLimitPolicy.RefillPeriod})
		if err != nil {
			return domain.ErrVerificationUnavailable
		}
		if !decision.Allowed {
			return domain.ErrVerificationLimited
		}
	}
	return nil
}

// sendCode renders the sign-in email and uses the borrowed mailer directly.
// Codes remain on the authentication path with its expiry and resend policies.
func (s *authService) sendCode(ctx context.Context, email, code string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	content, err := mailviews.SignIn(code, ttl)
	if err != nil {
		return err
	}
	rendered, err := s.mailRenderer.Render(content)
	if err != nil {
		return err
	}
	return s.mailer.Send(ctx, mail.Message{To: []mail.Address{{Email: email}}, Subject: rendered.Subject, TextBody: rendered.Text, HTMLBody: rendered.HTML})
}
