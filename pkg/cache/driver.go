package cache

import "fmt"

// Driver identifies a supported cache implementation. Validate at configuration boundaries.
type Driver string

const (
	DriverDisabled Driver = "disabled"
	DriverMemory   Driver = "memory"
	DriverDatabase Driver = "database"
	DriverRedis    Driver = "redis"
)

func (d Driver) Validate() error {
	switch d {
	case DriverDisabled, DriverMemory, DriverDatabase, DriverRedis:
		return nil
	default:
		return fmt.Errorf("unsupported cache driver %q", d)
	}
}
