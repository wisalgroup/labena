package enums

import "slices"

// SessionPlatform is owned by Identity. The zero value is invalid.
type SessionPlatform string

const (
	PlatformIOS     SessionPlatform = "ios"
	PlatformAndroid SessionPlatform = "android"
	PlatformWeb     SessionPlatform = "web"
)

// SessionPlatformValues returns all supported values in declaration order.
// The returned slice is independent and may be modified by the caller.
func SessionPlatformValues() []SessionPlatform {
	return []SessionPlatform{PlatformIOS, PlatformAndroid, PlatformWeb}
}

// Valid reports whether the value is supported. Decoding does not validate membership.
func (v SessionPlatform) Valid() bool { return slices.Contains(SessionPlatformValues(), v) }
