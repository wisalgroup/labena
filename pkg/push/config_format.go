package push

import "encoding/json"

// Diagnostic formatting never exposes the token or a borrowed client's internals.
func (c ExpoConfig) String() string   { return "push.ExpoConfig{AccessToken:[REDACTED]}" }
func (c ExpoConfig) GoString() string { return c.String() }
func (c ExpoConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{ AccessToken string }{})
}
