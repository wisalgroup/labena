package entities

// EntityType is a stable model discriminator for polymorphic ownership and routing.
// Persisted values must remain stable even when Go packages or structs are renamed.
type EntityType string

const (
	// UserEntityType identifies Identity users across media and notifications.
	UserEntityType EntityType = "user"
)
