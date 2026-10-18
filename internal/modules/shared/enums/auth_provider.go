package enums

import "slices"

// AuthProvider is owned by Identity. The zero value is invalid.
type AuthProvider string

const (
	ProviderGoogle AuthProvider = "google"
	ProviderApple  AuthProvider = "apple"
)

// AuthProviderValues returns all supported values in declaration order.
// The returned slice is independent and may be modified by the caller.
func AuthProviderValues() []AuthProvider {
	return []AuthProvider{ProviderGoogle, ProviderApple}
}

// Valid reports whether the value is supported. Decoding does not validate membership.
func (v AuthProvider) Valid() bool { return slices.Contains(AuthProviderValues(), v) }
