package valueobjects

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// Email represents an email address value object
type Email struct {
	value string
}

// NewEmail creates a new Email value object
func NewEmail(value string) (Email, error) {
	value = strings.TrimSpace(strings.ToLower(value))

	if value == "" {
		return Email{}, errors.New("email cannot be empty")
	}

	if len(value) > 254 || !emailRegex.MatchString(value) {
		return Email{}, errors.New("invalid email format")
	}

	return Email{value: value}, nil
}

func (e Email) Valid() bool {
	if e.value == "" || len(e.value) > 254 {
		return false
	}

	if !emailRegex.MatchString(e.value) {
		return false
	}
	return true
}

// Value implements driver.Valuer. Invalid zero values cannot be persisted.
func (e Email) Value() (driver.Value, error) {
	if !e.Valid() {
		return nil, errors.New("invalid email value")
	}
	return e.value, nil
}

var _ driver.Valuer = Email{}
var _ sql.Scanner = (*Email)(nil)

// Scan validates before replacing the address. SQL NULL is invalid for Email.
func (e *Email) Scan(src any) error {
	var value string
	switch src := src.(type) {
	case string:
		value = src
	case []byte:
		value = string(src)
	default:
		return fmt.Errorf("email: unsupported SQL value %T", src)
	}
	parsed, err := NewEmail(value)
	if err != nil {
		return err
	}
	*e = parsed
	return nil
}

// Domain returns the domain part of the email
func (e Email) Domain() string {
	parts := strings.Split(e.value, "@")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// LocalPart returns the local part of the email
func (e Email) LocalPart() string {
	parts := strings.Split(e.value, "@")
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

// Equal checks if two emails are equal
func (e Email) Equal(other Email) bool {
	return e.value == other.value
}

// String returns the string representation
func (e Email) String() string {
	return e.value
}

// MarshalJSON exposes the address, never the internal representation.
func (e Email) MarshalJSON() ([]byte, error) { return json.Marshal(e.value) }

// UnmarshalJSON validates and normalizes before replacing the value.
func (e *Email) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := NewEmail(value)
	if err != nil {
		return err
	}
	*e = parsed
	return nil
}
