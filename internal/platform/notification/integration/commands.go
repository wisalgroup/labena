package integration

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/pagination"
)

func newCommands(notifier *notification.Notifier) *cobra.Command {
	group := &cobra.Command{Use: "notifications", Short: "Notifications commands"}
	group.AddCommand(&cobra.Command{Use: "prune-subscriptions", Short: "Disable up to 1000 push registrations inactive for 90 days", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		count, err := notifier.PruneSubscriptions(cmd.Context(), time.Now().Add(-subscriptionMaxAge))
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(count)
	}})
	group.AddCommand(&cobra.Command{Use: "list RECIPIENT_TYPE RECIPIENT_ID", Short: "List recipient notifications (operator command)", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		recipient := notification.Recipient{Type: args[0], ID: args[1]}
		page, err := notifier.List(cmd.Context(), recipient, pagination.NewPagination(1, 50))
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(page)
	}})
	group.AddCommand(&cobra.Command{Use: "unread RECIPIENT_TYPE RECIPIENT_ID", Short: "Count unread notifications", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		recipient := notification.Recipient{Type: args[0], ID: args[1]}
		count, err := notifier.UnreadCount(cmd.Context(), recipient)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Count int `json:"count"`
		}{count})
	}})
	group.AddCommand(&cobra.Command{Use: "read RECIPIENT_TYPE RECIPIENT_ID NOTIFICATION_ID", Short: "Mark a recipient notification as read", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		recipient := notification.Recipient{Type: args[0], ID: args[1]}
		id, err := uuid.Parse(args[2])
		if err != nil {
			return err
		}
		return notifier.MarkRead(cmd.Context(), recipient, id)
	}})
	group.AddCommand(&cobra.Command{Use: "dispatch", Short: "Submit due persisted deliveries", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		n, err := notifier.Dispatch(cmd.Context())
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(n)
	}})
	group.AddCommand(&cobra.Command{Use: "delivery ID", Short: "Inspect delivery status", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		row, err := notifier.GetDelivery(cmd.Context(), id)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(row)
	}})
	group.AddCommand(&cobra.Command{Use: "retry ID", Short: "Retry a terminal failed delivery (may duplicate a provider effect)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		return notifier.RetryDelivery(cmd.Context(), id)
	}})
	var status string
	var page int
	deliveries := &cobra.Command{Use: "deliveries", Short: "List persisted delivery status (operator command)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if page < 1 {
			return apperror.Validation("Page must be positive.")
		}
		result, err := notifier.ListDeliveries(cmd.Context(), notification.DeliveryStatus(status), pagination.NewPagination(page, 50))
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}}
	deliveries.Flags().StringVar(&status, "status", "failed", "Filter pending, awaiting_receipt, sending, sent, skipped or failed; empty means all")
	deliveries.Flags().IntVar(&page, "page", 1, "Page number")
	group.AddCommand(deliveries)

	return group
}
