// Package mailviews defines Identity email content for the default branded layout.
package mailviews

import (
	"strings"

	"github.com/wisalgroup/labena/pkg/mailview"
)

// NewWelcome is Identity's mailable content builder. The shared Renderer supplies
// layout, logo, colors, footer, escaping and a plain-text alternative. This content
// can be rendered for a local preview, sent directly, or snapshotted by a Notifier.
func NewWelcome(name string) mailview.Email {
	greeting := "Welcome aboard."
	if name = strings.TrimSpace(name); name != "" {
		greeting = "Welcome, " + name + "."
	}
	return mailview.Email{
		Subject:   "Welcome to your account",
		Preheader: "Your account is ready. Here are your next steps.",
		Eyebrow:   "YOUR ACCOUNT",
		Title:     greeting,
		Paragraphs: []string{
			"Your account is ready to use.",
			"Review your profile and keep your sign-in methods up to date. You can review and sign out your active sessions from your account settings.",
		},
		Note: "We will never ask you to share a sign-in code by email.",
	}
}
