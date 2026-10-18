package database

import "fmt"

// Driver identifies a supported database implementation. Validate at configuration boundaries.
type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverMySQL    Driver = "mysql"
	DriverSQLite   Driver = "sqlite"
)

func (d Driver) Validate() error {
	switch d {
	case DriverPostgres, DriverMySQL, DriverSQLite:
		return nil
	default:
		return fmt.Errorf("database: unsupported driver %q", d)
	}
}
