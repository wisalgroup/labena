package platform_test

import (
	"strings"
	"testing"

	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/platform/media"
	"github.com/wisalgroup/labena/internal/platform/notification"
)

func TestPolymorphicReferencesShareSyntax(t *testing.T) {
	for _, tc := range []struct {
		name, kind, id string
		valid          bool
	}{
		{"user", string(entities.UserEntityType), "42", true},
		{"custom model", "catalog.product-v2", "SKU:青", true},
		{"limits", strings.Repeat("a", 64), strings.Repeat("x", 200), true},
		{"empty type", "", "42", false},
		{"uppercase type", "User", "42", false},
		{"numeric type", "1user", "42", false},
		{"channel syntax", "user:private", "42", false},
		{"long type", strings.Repeat("a", 65), "42", false},
		{"empty ID", "user", "", false},
		{"long ID", "user", strings.Repeat("x", 201), false},
		{"leading whitespace", "user", " 42", false},
		{"trailing whitespace", "user", "42\u00a0", false},
		{"control", "user", "4\x002", false},
		{"invalid UTF8", "user", "\xff", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]bool{
				"owner":     (media.Owner{Type: tc.kind, ID: tc.id}).Valid(),
				"recipient": (notification.Recipient{Type: tc.kind, ID: tc.id}).Valid(),
			}
			for name, valid := range values {
				if valid != tc.valid {
					t.Errorf("%s.Valid() = %t; want %t", name, valid, tc.valid)
				}
			}
		})
	}
}
