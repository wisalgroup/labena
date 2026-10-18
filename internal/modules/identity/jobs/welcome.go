// Package jobs defines Identity's native Asynq tasks and their serializable inputs.
package jobs

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/pkg/asynqx"
)

const WelcomeType = "identity.welcome.v1"

// Welcome carries only identity. The worker reloads account state and contact data;
// names, email addresses, credentials and notification content never enter this job.
type Welcome struct {
	UserID uuid.UUID `json:"user_id"`
}

// NewWelcome constructs a fresh task; enqueue through the app's borrowed client.
// Queue routing comes from application configuration. Retries are safe because
// the notification's occurrence key is stable across attempts.
func NewWelcome(userID uuid.UUID) (*asynq.Task, error) {
	if userID == uuid.Nil {
		return nil, errors.New("identity: welcome job requires a user ID")
	}
	data, err := json.Marshal(Welcome{UserID: userID})
	if err != nil {
		return nil, err
	}
	return asynqx.NewTask(WelcomeType, data, asynq.MaxRetry(5), asynq.Timeout(30*time.Second)), nil
}
