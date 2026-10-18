package logging

import (
	"context"
	"errors"
	"net"
)

// ErrorCategory returns a bounded diagnostic category without exposing error text.
// It recognizes wrapped causes. It is for reporting, not retry or business policy.
func ErrorCategory(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "timeout"
		}
		return "network"
	}
	return "other"
}
