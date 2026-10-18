package entities

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
)

func TestUserNameInvariants(t *testing.T) {
	email, _ := valueobjects.NewEmail("ada@example.com")
	for _, name := range []string{"bad\nname", strings.Repeat("界", 101)} {
		if _, err := NewUser(name, email); err == nil {
			t.Fatal("accepted invalid name")
		}
	}
	user, err := NewUser(" Ada ", email)
	if err != nil || user.Name != "Ada" {
		t.Fatal(user, err)
	}
	before := user.UpdatedAt
	if err := user.Rename(strings.Repeat("x", 101)); err == nil || user.Name != "Ada" || user.UpdatedAt != before {
		t.Fatal("invalid rename mutated user")
	}
	if err := user.Rename("Ada"); err != nil || user.UpdatedAt != before {
		t.Fatal("unchanged rename changed timestamp")
	}
	if err := user.Rename(" Grace "); err != nil || user.Name != "Grace" || user.UpdatedAt.Before(before) {
		t.Fatal("rename failed", err)
	}
}

func TestInvalidUserStateIdentity(t *testing.T) {
	err := fmt.Errorf("save: %w", (User{}).Validate())
	if !errors.Is(err, ErrInvalidUserState) {
		t.Fatal(err)
	}
}

func TestIncompleteProfile(t *testing.T) {
	email, _ := valueobjects.NewEmail("new@example.com")
	user, err := NewUser("", email)
	if err != nil || user.ProfileComplete() || user.Validate() != nil {
		t.Fatal("new incomplete profile", user, err)
	}
	if err := user.Rename("  "); err == nil {
		t.Fatal("blank profile update accepted")
	}
	if err := user.Rename("Ada"); err != nil || !user.ProfileComplete() {
		t.Fatal("profile not completed", err)
	}
}
