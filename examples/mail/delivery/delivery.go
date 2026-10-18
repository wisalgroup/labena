// Package delivery demonstrates queued mail using borrowed application clients.
package delivery

import (
	"context"

	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/pkg/mailview"
)

// EnqueueReport renders once before Redis accepts the immutable message snapshot.
// The caller authorizes and validates the destination before invoking this operation.
func EnqueueReport(ctx context.Context, mailer mail.Mailer, views *mailview.Renderer, address string) (*asynq.TaskInfo, error) {
	content, err := views.Render(mailview.Email{
		Subject: "Your monthly report is ready",
		Title:   "Report ready",
		Paragraphs: []string{
			"Your monthly report is available in your account.",
		},
		Action: &mailview.Action{Label: "View report", URL: "https://example.com/reports"},
	})
	if err != nil {
		return nil, err
	}
	return mailer.Enqueue(ctx, mail.Message{
		To:       []mail.Address{{Email: address}},
		Subject:  content.Subject,
		HTMLBody: content.HTML,
		TextBody: content.Text,
	})
}
