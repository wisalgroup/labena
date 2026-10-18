package enums

import "slices"

// VerificationChannel is owned by Identity. The zero value is invalid.
type VerificationChannel string

const (
	ChannelEmail VerificationChannel = "email"
	ChannelSMS   VerificationChannel = "sms"
)

// VerificationChannelValues returns all supported values in declaration order.
// The returned slice is independent and may be modified by the caller.
func VerificationChannelValues() []VerificationChannel {
	return []VerificationChannel{ChannelEmail, ChannelSMS}
}

// Valid reports whether the value is supported. Decoding does not validate membership.
func (v VerificationChannel) Valid() bool { return slices.Contains(VerificationChannelValues(), v) }
