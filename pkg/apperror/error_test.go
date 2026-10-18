package apperror

import (
	"errors"
	"fmt"
	"testing"
)

func TestWrappingAndFieldOwnership(t *testing.T) {
	sentinel := Conflict("Email already registered.")
	wrapped := fmt.Errorf("save user: %w", sentinel)
	var found *Error
	if !errors.Is(wrapped, sentinel) || !errors.As(wrapped, &found) || found.Code() != CodeConflict {
		t.Fatal("lost error identity")
	}
	source := map[string][]string{"email": {"Required."}}
	invalid := InvalidFields(source)
	source["email"][0] = "changed"
	returned := invalid.Fields()
	returned["email"][0] = "changed again"
	if invalid.Code() != CodeValidation || invalid.Fields()["email"][0] != "Required." {
		t.Fatal("mutable validation error")
	}
	if errors.Is(sentinel, Conflict("Email already registered.")) {
		t.Fatal("unrelated errors share identity")
	}
}
