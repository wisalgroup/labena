package enums

import "slices"

// VerificationPurpose is owned by Identity. The zero value is invalid.
type VerificationPurpose string

const (
	PurposeVerifyEmail   VerificationPurpose = "verify_email"
	PurposeRegistration  VerificationPurpose = "registration"
	PurposeSignIn        VerificationPurpose = "sign_in"
	PurposeLinkContact   VerificationPurpose = "link_contact"
	PurposeChangeContact VerificationPurpose = "change_contact"
	PurposeRecovery      VerificationPurpose = "recovery"
)

// VerificationPurposeValues returns all supported values in declaration order.
// The returned slice is independent and may be modified by the caller.
func VerificationPurposeValues() []VerificationPurpose {
	return []VerificationPurpose{PurposeVerifyEmail, PurposeRegistration, PurposeSignIn, PurposeLinkContact, PurposeChangeContact, PurposeRecovery}
}

// Valid reports whether the value is supported. Decoding does not validate membership.
func (v VerificationPurpose) Valid() bool { return slices.Contains(VerificationPurposeValues(), v) }
