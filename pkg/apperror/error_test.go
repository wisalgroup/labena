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
	if !errors.Is(wrapped, sentinel) || !errors.As(wrapped, &found) || found.Kind() != KindConflict {
		t.Fatal("lost error identity")
	}
	source := map[string][]string{"email": {"Required."}}
	invalid := InvalidFields(source)
	source["email"][0] = "changed"
	returned := invalid.Fields()
	returned["email"][0] = "changed again"
	if invalid.Kind() != KindValidation || invalid.Fields()["email"][0] != "Required." {
		t.Fatal("mutable validation error")
	}
	if errors.Is(sentinel, Conflict("Email already registered.")) {
		t.Fatal("unrelated errors share identity")
	}
}

// Published codes remain usable by consumers in constant declarations.
const consumerValidationCode = KindValidation

func TestWithTypePreservesIdentityAndFields(t *testing.T) {
	base := InvalidFields(map[string][]string{"email": {"Required."}})
	specific := base.WithType("urn:example:error:email-required")
	wrapped := fmt.Errorf("save: %w", specific)
	var found *Error
	if !errors.Is(wrapped, base) || !errors.As(wrapped, &found) || found != specific {
		t.Fatal("lost application error identity")
	}
	if base.Type() != "" || specific.Type() != "urn:example:error:email-required" || specific.Kind() != KindValidation {
		t.Fatal("type decoration changed classification or mutated original")
	}
	specific.Fields()["email"][0] = "changed"
	if base.Fields()["email"][0] != "Required." || specific.Fields()["email"][0] != "Required." {
		t.Fatal("shared validation fields became mutable")
	}
	reset := specific.WithType("")
	if reset.Type() != "" || !errors.Is(reset, base) || specific.Type() == "" {
		t.Fatal("clearing type changed existing error")
	}
	if base.Unwrap() != nil {
		t.Fatal("undecorated error has a cause")
	}
}

func TestWithTypeRequiresAbsoluteURI(t *testing.T) {
	for _, value := range []string{"urn:example:error:missing", "https://example.com/problems/missing", ""} {
		if got := NotFound("Missing.").WithType(value).Type(); got != value {
			t.Fatal(got)
		}
	}
	for _, value := range []string{"missing", "/problems/missing", "urn:", "urn:example:bad value", "urn:example:bad\nvalue", "https://example.com/%zz"} {
		t.Run(value, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("accepted invalid static problem URI")
				}
			}()
			NotFound("Missing.").WithType(value)
		})
	}
}

func TestDefinitionAndFluentErrors(t *testing.T) {
	base := Define(Definition{Kind: KindNotFound, Code: "USER_NOT_FOUND", Type: "https://example.com/problems/user-not-found", Title: "User Not Found"})
	fluent := NotFound("").WithTitle("User Not Found").WithType(base.Type()).WithCode("USER_NOT_FOUND")
	for _, e := range []*Error{base, fluent} {
		if e.Kind() != KindNotFound || e.Code() != "USER_NOT_FOUND" || e.Title() != "User Not Found" || e.Detail() != "" || e.Type() != base.Type() {
			t.Fatal(e)
		}
		detailed := e.WithDetail("User 123 does not exist")
		if !errors.Is(detailed, e) || e.Detail() != "" || detailed.Detail() != "User 123 does not exist" {
			t.Fatal("mutated definition or lost identity")
		}
		reset := detailed.WithCode("").WithTitle("").WithType("").WithDetail("")
		if reset.Code() != "not_found" || reset.Title() != "" || reset.Type() != "" || reset.Detail() != "" || !errors.Is(reset, e) {
			t.Fatal("fallback or identity lost")
		}
	}
}

func TestWithFieldsCopiesAndRestrictsValidation(t *testing.T) {
	base := Validation("")
	fields := map[string][]string{"email": {"Required."}}
	e := base.WithFields(fields).WithCode("EMAIL_REQUIRED").WithDetail("Check email.")
	fields["email"][0] = "changed"
	if base.Fields() != nil || e.Fields()["email"][0] != "Required." || e.Kind() != KindValidation || !errors.Is(e, base) {
		t.Fatal("field ownership lost")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("allowed fields on non-validation error")
		}
	}()
	NotFound("").WithFields(fields)
}

func TestDefinitionValidation(t *testing.T) {
	for _, def := range []Definition{
		{}, {Kind: "unknown", Code: "X", Type: "urn:example:x", Title: "X"},
		{Kind: KindNotFound, Type: "urn:example:x", Title: "X"},
		{Kind: KindNotFound, Code: "X", Title: "X"},
		{Kind: KindNotFound, Code: "X", Type: "urn:example:x"},
	} {
		t.Run(fmt.Sprint(def), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("accepted incomplete definition")
				}
			}()
			Define(def)
		})
	}
}

func TestSharedDefinitionConcurrentDecoration(t *testing.T) {
	base := Validation("").WithCode("INVALID_PROFILE")
	for i := range 20 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			e := base.WithDetail(fmt.Sprint(i)).WithFields(map[string][]string{"name": {"Required."}}).WithTitle("Invalid Profile")
			if !errors.Is(e, base) || base.Detail() != "" || base.Title() != "" || base.Fields() != nil || e.Detail() != fmt.Sprint(i) {
				t.Fatal("shared error mutated")
			}
		})
	}
}
