package mailviews

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/wisalgroup/labena/pkg/mailview"
)

// SignIn returns content for both first-time and returning users.
func SignIn(code string, ttl time.Duration) (mailview.Email, error) {
	if len(code) != 6 || ttl <= 0 {
		return mailview.Email{}, errors.New("identity mail: six-digit code and positive lifetime required")
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return mailview.Email{}, errors.New("identity mail: code must contain digits only")
		}
	}
	minutes := int(math.Ceil(ttl.Minutes()))
	unit := "minutes"
	if minutes == 1 {
		unit = "minute"
	}
	return mailview.Email{
		Subject:    "Your sign-in code",
		Preheader:  "Use your one-time code to sign in.",
		Eyebrow:    "ACCOUNT SECURITY",
		Title:      "Continue with email.",
		Paragraphs: []string{"Enter this code in the app to continue. If you do not have an account, one will be created after you confirm your email. This code cannot change an existing email address."},
		Code:       &mailview.Code{Value: code, Label: "YOUR SIGN-IN CODE", Hint: fmt.Sprintf("Expires in %d %s · One-time use", minutes, unit)},
		Note:       "Never share this code. Our team will never ask for it. If you didn’t request this email, you can safely ignore it.",
	}, nil
}
