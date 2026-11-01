package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/platform/auth"
	"github.com/wisalgroup/labena/pkg/apperror"
)

func TestPrincipalContext(t *testing.T) {
	principal := auth.Principal{UserID: uuid.New(), SessionID: uuid.New()}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := auth.WithPrincipal(parent, principal)
	got, err := auth.FromContext(ctx)
	if err != nil || got != principal {
		t.Fatal(got, err)
	}
	got.UserID = uuid.New()
	if userID, err := auth.CurrentUserID(ctx); err != nil || userID != principal.UserID {
		t.Fatal("principal changed through returned copy", userID, err)
	}
	if err := auth.RequireCurrentUser(ctx, principal.UserID); err != nil {
		t.Fatal(err)
	}
	assertKind(t, auth.RequireCurrentUser(ctx, uuid.New()), apperror.KindNotFound)
	_, err = auth.FromContext(parent)
	assertKind(t, err, apperror.KindUnauthenticated)
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("authentication lost cancellation")
	}
}

func TestPrincipalContextFailsClosed(t *testing.T) {
	for _, principal := range []auth.Principal{
		{}, {UserID: uuid.New()}, {SessionID: uuid.New()},
	} {
		ctx := auth.WithPrincipal(context.Background(), principal)
		got, err := auth.FromContext(ctx)
		if got != (auth.Principal{}) {
			t.Fatal("exposed incomplete principal", got)
		}
		assertKind(t, err, apperror.KindUnauthenticated)
		id, err := auth.CurrentUserID(ctx)
		if id != uuid.Nil {
			t.Fatal(id)
		}
		assertKind(t, err, apperror.KindUnauthenticated)
		assertKind(t, auth.RequireCurrentUser(ctx, principal.UserID), apperror.KindUnauthenticated)
	}
	assertKind(t, auth.RequireCurrentUser(context.Background(), uuid.New()), apperror.KindUnauthenticated)
}

func assertKind(t *testing.T, err error, want apperror.Kind) {
	t.Helper()
	var public *apperror.Error
	if !errors.As(err, &public) || public.Kind() != want {
		t.Fatalf("got %v; want %s", err, want)
	}
}
