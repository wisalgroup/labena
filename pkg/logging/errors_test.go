package logging

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestErrorCategory(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "none"}, {fmt.Errorf("private: %w", context.Canceled), "canceled"},
		{fmt.Errorf("private: %w", context.DeadlineExceeded), "timeout"},
		{&net.OpError{Op: "private", Err: errors.New("private address")}, "network"},
		{errors.New("private credential"), "other"},
	} {
		if got := ErrorCategory(tc.err); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}
